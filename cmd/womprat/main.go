package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"tailscale.com/tsnet"
)

//go:embed frontend/*
var frontendFS embed.FS

var useExitNode = false

const appName = "womprat"

var (
	version       = "0.3.0"
	commit        = "dev"
	tabIDSequence uint64
)

func newTabID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixMilli(), atomic.AddUint64(&tabIDSequence, 1))
}

type pendingSSH struct {
	host string
	user string
	port int
	cols int
	rows int
}

type Tab struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // "terminal", "browser", "settings", "vnc"
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Favicon string `json:"favicon,omitempty"`
	Host    string `json:"host,omitempty"`
	User    string `json:"user,omitempty"`
	Port    int    `json:"port,omitempty"`
}

type shellWebView interface {
	Navigate(string)
	Eval(string)
	Resize()
}

type browserContentView interface {
	Navigate(string)
	GoBack()
	GoForward()
	Reload()
	Show()
	Hide()
	Destroy()
}

type browserContentManager interface {
	Ensure(tabID string) browserContentView
	Get(tabID string) browserContentView
	Show(tabID string)
	HideAll()
	Destroy(tabID string)
}

type App struct {
	mu           sync.Mutex
	config       *AppConfig
	tsServer     *tsnet.Server
	tabs         []Tab
	activeTab    string
	sshConns     map[string]*ssh.Client
	pendingAuth  map[string]*pendingSSH
	sessionToken string
	locked       bool
	webview      shellWebView
	contentViews browserContentManager
	dispatch     func(func())
	serverPort   int
	lastCloseAt  time.Time
	lastCloseTab string
}

func main() {
	cfg, err := LoadConfig()
	if err != nil {
		cfg = defaultConfig()
	}
	setupLogging(cfg.DebugLog)
	if err != nil {
		log.Printf("config load failed, using defaults: %v", err)
	}
	token, err := generateSessionToken()
	if err != nil {
		log.Fatalf("session token generation failed: %v", err)
	}
	app := &App{
		config:       cfg,
		sshConns:     make(map[string]*ssh.Client),
		pendingAuth:  make(map[string]*pendingSSH),
		sessionToken: token,
		locked:       shouldStartLocked(cfg),
	}
	useExitNode = cfg.ExitNode != ""

	// Start local HTTP server for API + shell pages
	mux := http.NewServeMux()
	app.registerRoutes(mux)
	app.registerSettingsRoutes(mux)
	app.registerBrowserRoutes(mux)
	app.registerDownloadRoutes(mux)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	app.serverPort = listener.Addr().(*net.TCPAddr).Port
	logStartupBanner(app)
	go serveLocalHTTP(listener, mux)

	// Start SOCKS5 proxy (port must be open before WebView2 starts)
	startSOCKS5Listener(app)

	// Start Tailscale only after the configured unlock gate is satisfied.
	if !app.locked {
		if err := app.startTailscale(); err != nil {
			log.Printf("Tailscale start failed: %v (will prompt for key)", err)
		}
	}

	shellURL := fmt.Sprintf("http://127.0.0.1:%d/", app.serverPort)
	runGUI(app, shellURL)
}

func serveLocalHTTP(listener net.Listener, handler http.Handler) {
	if err := http.Serve(listener, handler); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("local HTTP server stopped: %v", err)
	}
}

func jsString(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func (a *App) evalShell(format string, args ...interface{}) {
	if a.webview == nil {
		return
	}
	a.webview.Eval(fmt.Sprintf(format, args...))
}

func (a *App) onUIThread(fn func()) {
	// Native WebView2 creation pumps a nested message loop. Running it directly
	// from a JS<->Go binding callback (inside a WebView2 COM event) re-enters the
	// message loop and hangs. Dispatch defers work onto the wrapper run loop so it
	// executes outside the re-entrant callback.
	if a.dispatch != nil {
		a.dispatch(fn)
	} else {
		fn()
	}
}

func (a *App) hideBrowserContentOnUI() {
	if a.contentViews == nil {
		return
	}
	a.onUIThread(func() { a.contentViews.HideAll() })
}

func (a *App) showFullShellOnUI() {
	a.hideBrowserContentOnUI()
}

func (a *App) destroyContentOnUI(tabID string) {
	if a.contentViews == nil {
		return
	}
	a.onUIThread(func() { a.contentViews.Destroy(tabID) })
}

func (a *App) navigateBrowser(url string) {
	log.Printf("tab: navigateBrowser %s (active=%s)", url, a.activeTab)
	if a.openSpecialURL(url) {
		return
	}
	normalized, err := normalizeBrowserURL(url)
	if err != nil {
		log.Printf("browser navigation rejected %q: %v", url, err)
		return
	}
	url = normalized
	var tabID string
	a.mu.Lock()
	for i, t := range a.tabs {
		if t.ID == a.activeTab && t.Type == "browser" {
			a.tabs[i].URL = url
			a.tabs[i].Title = url
			tabID = t.ID
			break
		}
	}
	a.mu.Unlock()
	a.persistOpenTabs()
	if tabID == "" {
		return
	}
	a.evalShell("window.showBrowserTab(%s,%s,{skipNative:true})", jsString(tabID), jsString(url))
	if a.contentViews != nil {
		a.onUIThread(func() {
			view := a.contentViews.Ensure(tabID)
			a.contentViews.Show(tabID)
			view.Navigate(url)
		})
	}
}

func (a *App) activeContentView() browserContentView {
	if a.contentViews == nil {
		return nil
	}
	a.mu.Lock()
	active := a.activeTab
	a.mu.Unlock()
	if active == "" {
		return nil
	}
	return a.contentViews.Get(active)
}

func (a *App) updateActiveBrowserTitle(title, url, favicon string) {
	title = sanitizeBrowserTitle(title)
	url = strings.TrimSpace(url)
	favicon = sanitizeFaviconURL(favicon)
	// Chromium's internal error pages report chrome-error://chromewebdata/ as the
	// current URL. Never persist that over the user's requested URL: a transient
	// routing/DNS failure should not destroy the tab's navigable address.
	if url != "" && !isBrowserURL(url) {
		url = ""
		favicon = ""
	}
	if title == "" {
		title = url
	}
	if title == "" {
		return
	}
	changed := false
	a.mu.Lock()
	for i := range a.tabs {
		if a.tabs[i].ID == a.activeTab && a.tabs[i].Type == "browser" {
			if a.tabs[i].Title != title {
				a.tabs[i].Title = title
				changed = true
			}
			if url != "" && a.tabs[i].URL != url {
				a.tabs[i].URL = url
				changed = true
			}
			if favicon != "" && a.tabs[i].Favicon != favicon {
				a.tabs[i].Favicon = favicon
				changed = true
			}
			break
		}
	}
	a.mu.Unlock()
	if changed {
		a.persistOpenTabs()
	}
}

func (a *App) persistOpenTabs() {
	a.mu.Lock()
	saved := make([]SavedTab, 0, len(a.tabs))
	for _, t := range a.tabs {
		if t.Type == "settings" {
			continue
		}
		saved = append(saved, SavedTab{Type: t.Type, Title: t.Title, Host: t.Host, User: t.User, Port: t.Port, URL: t.URL, Favicon: t.Favicon})
	}
	a.config.OpenTabs = saved
	cfg := a.config
	a.mu.Unlock()
	if err := SaveConfig(cfg); err != nil {
		log.Printf("persist open tabs failed: %v", err)
	}
}

func (a *App) switchTab(tabID string) {
	log.Printf("tab: switchTab %s", tabID)
	a.mu.Lock()
	var tab *Tab
	for i := range a.tabs {
		if a.tabs[i].ID == tabID {
			tab = &a.tabs[i]
			break
		}
	}
	if tab == nil {
		a.mu.Unlock()
		return
	}
	a.activeTab = tabID
	a.mu.Unlock()

	switch tab.Type {
	case "browser":
		a.evalShell("window.activateTab(%s,{skipNative:true})", jsString(tabID))
		if a.contentViews != nil {
			url := tab.URL
			a.onUIThread(func() {
				view := a.contentViews.Get(tabID)
				if view == nil {
					view = a.contentViews.Ensure(tabID)
					if url != "" {
						view.Navigate(url)
					}
				}
				a.contentViews.Show(tabID)
			})
		}
	case "terminal", "settings", "vnc", "rdp":
		a.showFullShellOnUI()
		a.evalShell("window.activateTab(%s,{skipNative:true})", jsString(tabID))
	}
}

func clampTabIndex(toIndex, count int) int {
	if count <= 0 {
		return 0
	}
	if toIndex < 0 {
		return 0
	}
	if toIndex >= count {
		return count - 1
	}
	return toIndex
}

func (a *App) reorderTab(tabID string, toIndex int) {
	a.mu.Lock()
	from := -1
	for i, t := range a.tabs {
		if t.ID == tabID {
			from = i
			break
		}
	}
	if from < 0 {
		a.mu.Unlock()
		return
	}
	toIndex = clampTabIndex(toIndex, len(a.tabs))
	if from == toIndex {
		a.mu.Unlock()
		return
	}
	tab := a.tabs[from]
	a.tabs = append(a.tabs[:from], a.tabs[from+1:]...)
	if toIndex > from {
		toIndex--
	}
	if toIndex < 0 {
		toIndex = 0
	}
	if toIndex >= len(a.tabs) {
		a.tabs = append(a.tabs, tab)
	} else {
		a.tabs = append(a.tabs[:toIndex], append([]Tab{tab}, a.tabs[toIndex:]...)...)
	}
	a.mu.Unlock()
	a.persistOpenTabs()
}

func (a *App) forgetTab(tabID string) {
	a.mu.Lock()
	newTabs := []Tab{}
	for _, t := range a.tabs {
		if t.ID != tabID {
			newTabs = append(newTabs, t)
		}
	}
	a.tabs = newTabs
	if a.activeTab == tabID {
		a.activeTab = ""
	}
	a.mu.Unlock()
	a.destroyContentOnUI(tabID)
	a.persistOpenTabs()
}

func (a *App) closeTab(tabID string) {
	a.mu.Lock()
	now := time.Now()
	if tabID == a.lastCloseTab && !a.lastCloseAt.IsZero() && now.Sub(a.lastCloseAt) < 250*time.Millisecond {
		log.Printf("ignoring duplicate tab close for %s", tabID)
		a.mu.Unlock()
		return
	}
	a.lastCloseAt = now
	a.lastCloseTab = tabID
	oldActive := a.activeTab
	closedIndex := -1
	newTabs := []Tab{}
	for i, t := range a.tabs {
		if t.ID == tabID {
			closedIndex = i
			continue
		}
		newTabs = append(newTabs, t)
	}
	if closedIndex < 0 {
		a.mu.Unlock()
		return
	}
	a.tabs = newTabs
	if oldActive == tabID {
		if len(newTabs) > 0 {
			nextIndex := closedIndex
			if nextIndex >= len(newTabs) {
				nextIndex = len(newTabs) - 1
			}
			if nextIndex < 0 {
				nextIndex = 0
			}
			a.activeTab = newTabs[nextIndex].ID
		} else {
			a.activeTab = ""
		}
	}
	nextActive := a.activeTab
	a.mu.Unlock()
	a.destroyContentOnUI(tabID)
	a.persistOpenTabs()

	if len(newTabs) == 0 || nextActive == "" {
		a.goHome()
	} else if oldActive == tabID {
		a.switchTab(nextActive)
	}
}

func (a *App) newBrowserTab(url string) {
	log.Printf("tab: newBrowserTab %s", url)
	if a.openSpecialURL(url) {
		return
	}
	normalized, err := normalizeBrowserURL(url)
	if err != nil {
		log.Printf("new browser tab rejected %q: %v", url, err)
		return
	}
	url = normalized
	tabID := newTabID("browser")
	a.mu.Lock()
	a.tabs = append(a.tabs, Tab{ID: tabID, Type: "browser", Title: url, URL: url})
	a.activeTab = tabID
	a.mu.Unlock()
	a.persistOpenTabs()
	a.evalShell("window.showBrowserTab(%s,%s,{skipNative:true})", jsString(tabID), jsString(url))
	if a.contentViews != nil {
		a.onUIThread(func() {
			view := a.contentViews.Ensure(tabID)
			a.contentViews.Show(tabID)
			view.Navigate(url)
		})
	}
}

func (a *App) openSpecialURL(raw string) bool {
	text := strings.TrimSpace(raw)
	if strings.EqualFold(text, "settings:") || strings.EqualFold(text, "settings") {
		a.openSettingsTab()
		return true
	}
	target, err := parseCustomURL(text)
	if err != nil {
		return false
	}
	switch target.Scheme {
	case "ssh":
		user := target.User
		if user == "" {
			user = "root"
		}
		a.newTerminalTab(target.Host, user, target.Port)
		return true
	case "vnc":
		a.newVNCTab(text)
		return true
	case "rdp":
		a.newRDPTab(text)
		return true
	default:
		return false
	}
}

func (a *App) openSettingsTab() {
	log.Printf("tab: openSettingsTab")
	tab := Tab{ID: "settings", Type: "settings", Title: "Settings", URL: "settings:"}
	a.mu.Lock()
	a.tabs = upsertTab(a.tabs, tab)
	a.activeTab = tab.ID
	a.mu.Unlock()
	a.hideBrowserContentOnUI()
	a.evalShell("window.activateTab(%s,{skipNative:true})", jsString(tab.ID))
}

func (a *App) newVNCTab(raw string) {
	text := strings.TrimSpace(raw)
	if text != "" && !strings.Contains(text, "://") {
		text = "vnc://" + text
	}
	custom, err := parseCustomURL(text)
	if err != nil || custom.Scheme != "vnc" {
		log.Printf("vnc: invalid target %q: %v", raw, err)
		return
	}
	tabID := newTabID("vnc")
	url := custom.canonicalURL()
	a.mu.Lock()
	a.tabs = upsertTab(a.tabs, Tab{ID: tabID, Type: "vnc", Title: url, URL: url, Host: custom.Host, Port: custom.Port})
	a.activeTab = tabID
	a.mu.Unlock()
	a.persistOpenTabs()
	a.hideBrowserContentOnUI()
	a.evalShell("window.activateTab(%s,{skipNative:true})", jsString(tabID))
}

func (a *App) newRDPTab(raw string) {
	text := strings.TrimSpace(raw)
	if text != "" && !strings.Contains(text, "://") {
		text = "rdp://" + text
	}
	custom, err := parseCustomURL(text)
	if err != nil || custom.Scheme != "rdp" {
		log.Printf("rdp: invalid target %q: %v", raw, err)
		return
	}
	tabID := newTabID("rdp")
	url := custom.canonicalURL()
	a.mu.Lock()
	a.tabs = upsertTab(a.tabs, Tab{ID: tabID, Type: "rdp", Title: url, URL: url, Host: custom.Host, User: custom.User, Port: custom.Port})
	a.activeTab = tabID
	a.mu.Unlock()
	a.persistOpenTabs()
	a.hideBrowserContentOnUI()
	a.evalShell("window.activateTab(%s,{skipNative:true})", jsString(tabID))
}

func (a *App) newTerminalTab(host, user string, port int) {
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	tabID := newTabID("term")
	a.mu.Lock()
	a.tabs = upsertTab(a.tabs, Tab{ID: tabID, Type: "terminal", Title: host, Host: host, User: user, Port: port})
	a.activeTab = tabID
	a.mu.Unlock()
	a.persistOpenTabs()
	a.hideBrowserContentOnUI()
	a.evalShell("window.activateTab(%s,{skipNative:true})", jsString(tabID))
}

func (a *App) registerLocalTab(tabJSON string) {
	if len(tabJSON) > 64*1024 {
		return
	}
	var tab Tab
	if err := json.Unmarshal([]byte(tabJSON), &tab); err != nil || !validTabID(tab.ID) {
		return
	}
	if tab.Type == "settings" {
		tab = Tab{ID: "settings", Type: "settings", Title: "Settings", URL: "settings:"}
	} else {
		a.mu.Lock()
		for _, existing := range a.tabs {
			if existing.ID == tab.ID {
				if tab.Type == "" {
					tab.Type = existing.Type
				}
				if tab.Title == "" {
					tab.Title = existing.Title
				}
				if tab.Host == "" {
					tab.Host = existing.Host
				}
				if tab.User == "" {
					tab.User = existing.User
				}
				if tab.Port == 0 {
					tab.Port = existing.Port
				}
				if tab.URL == "" {
					tab.URL = existing.URL
				}
				if tab.Favicon == "" {
					tab.Favicon = existing.Favicon
				}
				break
			}
		}
		a.mu.Unlock()
		saved, ok := sanitizeSavedTab(SavedTab{Type: tab.Type, Title: tab.Title, Host: tab.Host, User: tab.User, Port: tab.Port, URL: tab.URL, Favicon: tab.Favicon})
		if !ok {
			return
		}
		tab.Type, tab.Title, tab.Host, tab.User, tab.Port, tab.URL, tab.Favicon = saved.Type, saved.Title, saved.Host, saved.User, saved.Port, saved.URL, saved.Favicon
	}
	a.mu.Lock()
	a.tabs = upsertTab(a.tabs, tab)
	a.activeTab = tab.ID
	a.mu.Unlock()
	a.persistOpenTabs()
}

func validTabID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 || strings.ContainsAny(id, " /\\?#") {
		return false
	}
	return true
}

func upsertTab(tabs []Tab, tab Tab) []Tab {
	for i := range tabs {
		if tabs[i].ID == tab.ID {
			if tab.Favicon == "" {
				tab.Favicon = tabs[i].Favicon
			}
			if tab.URL == "" {
				tab.URL = tabs[i].URL
			}
			if tab.Title == "" {
				tab.Title = tabs[i].Title
			}
			tabs[i] = tab
			return tabs
		}
	}
	return append(tabs, tab)
}

func (a *App) clearActiveTab() {
	a.mu.Lock()
	a.activeTab = ""
	a.mu.Unlock()
	a.hideBrowserContentOnUI()
}

func (a *App) goHome() {
	a.clearActiveTab()
	shellURL := fmt.Sprintf("http://127.0.0.1:%d/?v=%d", a.serverPort, time.Now().UnixMilli())
	a.webview.Navigate(shellURL)
}

func (a *App) startTailscale() error {
	authKey, err := GetCredential("tailscale-key")
	if err != nil || authKey == "" {
		return fmt.Errorf("no tailscale auth key")
	}

	ts := &tsnet.Server{
		Hostname:  appName,
		AuthKey:   authKey,
		Dir:       tsnetStateDir(),
		Ephemeral: false,
	}
	if _, err := ts.Up(context.Background()); err != nil {
		ts.Close()
		return err
	}

	a.mu.Lock()
	old := a.tsServer
	a.tsServer = ts
	a.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

func tsnetStateDir() string {
	dir, _ := os.UserConfigDir()
	return fmt.Sprintf("%s/%s/tsnet", dir, appName)
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", a.serveFrontend)
	mux.HandleFunc("/api/auth/status", a.authMiddleware(a.handleAuthStatus))
	mux.HandleFunc("/api/auth/unlock", a.authMiddleware(a.handleUnlock))
	mux.HandleFunc("/api/auth/save-key", a.authMiddleware(a.handleSaveKey))
	mux.HandleFunc("/api/tailscale/status", a.authMiddleware(a.handleTSStatus))
	mux.HandleFunc("/api/tailscale/peers", a.authMiddleware(a.handleTSPeers))
	mux.HandleFunc("/api/about", a.authMiddleware(a.handleAbout))
	mux.HandleFunc("/api/logs", a.authMiddleware(a.handleLogs))
	mux.HandleFunc("/api/settings/debug-log", a.authMiddleware(a.handleDebugLog))
	mux.HandleFunc("/api/ssh/connect", a.authMiddleware(a.handleSSHConnect))
	mux.HandleFunc("/api/ssh/resize", a.authMiddleware(a.handleSSHResize))
	mux.HandleFunc("/api/vnc/ws", a.authMiddleware(a.handleVNCWebSocket))
	mux.HandleFunc("/api/rdp/ws", a.authMiddleware(a.handleRDPWebSocket))
	mux.HandleFunc("/api/ssh/ws", a.authMiddleware(a.handleSSHWebSocketFull))
	mux.HandleFunc("/api/ssh/auth-password", a.authMiddleware(a.handleSSHAuthPassword))
}

func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (a *App) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Session-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != a.sessionToken {
			httpError(w, 403, "Forbidden", "Invalid session token")
			return
		}
		if a.isLocked() && r.URL.Path != "/api/auth/status" && r.URL.Path != "/api/auth/unlock" {
			httpError(w, 423, "Locked", "Unlock womprat before accessing protected settings or credentials")
			return
		}
		next(w, r)
	}
}

func (a *App) isLocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.locked
}

func frontendAssetPath(requestPath string) (string, bool) {
	if requestPath == "" || requestPath == "/" {
		return "frontend/index.html", true
	}
	if !strings.HasPrefix(requestPath, "/") || strings.Contains(requestPath, "\\") || strings.Contains(requestPath, "\x00") || strings.HasPrefix(requestPath, "/api/") || strings.Contains(requestPath, "/../") || strings.HasSuffix(requestPath, "/..") {
		return "", false
	}
	cleaned := path.Clean(requestPath)
	if cleaned == "." || cleaned == "/" {
		return "frontend/index.html", true
	}
	if strings.HasPrefix(cleaned, "/../") || cleaned == "/.." || strings.Contains(cleaned, "/../") {
		return "", false
	}
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || strings.HasPrefix(cleaned, "..") {
		return "", false
	}
	return "frontend/" + cleaned, true
}

func (a *App) serveFrontend(w http.ResponseWriter, r *http.Request) {
	// The shell changes rapidly during development and must never come from
	// WebView2's persistent HTTP cache; stale shell JS can resurrect old iframe
	// code paths and trigger frame-ancestors CSP failures.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path, ok := frontendAssetPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data, err := frontendFS.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(path, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Internal shell pages may frame only other local shell pages (settings).
		// External web content must be direct WebView navigation, never iframe.
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob:; connect-src 'self' ws: http://127.0.0.1:*; frame-src 'self' http://127.0.0.1:*; img-src 'self' data: blob: http: https:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline' 'unsafe-eval'")
		content := strings.Replace(string(data), "</head>",
			fmt.Sprintf(`<script>window.__SESSION_TOKEN="%s";window.__PORT=%d;</script></head>`, a.sessionToken, a.serverPort), 1)
		w.Write([]byte(content))
		return
	}
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".mjs") {
		w.Header().Set("Content-Type", "application/javascript")
	} else if strings.HasSuffix(path, ".wasm") {
		w.Header().Set("Content-Type", "application/wasm")
	} else if strings.HasSuffix(path, ".css") {
		w.Header().Set("Content-Type", "text/css")
	} else if strings.HasSuffix(path, ".ttf") {
		w.Header().Set("Content-Type", "font/ttf")
	}
	w.Write(data)
}

func (a *App) ts() *tsnet.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tsServer
}

func (a *App) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	_, err := GetCredential("tailscale-key")
	a.mu.Lock()
	unlockMethod := a.config.UnlockMethod
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hasKey":       err == nil,
		"connected":    a.ts() != nil,
		"locked":       a.isLocked(),
		"unlockMethod": unlockMethod,
	})
}

func (a *App) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	if ok, err := verifyMasterPassword(body.Password); err != nil || !ok {
		httpError(w, 401, "Unlock failed", "Invalid master password")
		return
	}
	a.mu.Lock()
	a.locked = false
	a.mu.Unlock()
	if err := a.startTailscale(); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "unlocked", "tailscale": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "unlocked"})
}

func (a *App) handleSaveKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	if err := validateTailscaleAuthKeyInput(body.Key); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := SaveCredential("tailscale-key", body.Key); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := a.startTailscale(); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "saved", "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "connected"})
}

func (a *App) handleTSStatus(w http.ResponseWriter, r *http.Request) {
	ts := a.ts()
	if ts == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
		return
	}
	lc, err := ts.LocalClient()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	status, err := lc.Status(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "connected",
		"hostname": status.Self.HostName,
		"ip":       status.TailscaleIPs,
	})
}

func (a *App) handleTSPeers(w http.ResponseWriter, r *http.Request) {
	ts := a.ts()
	if ts == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	lc, err := ts.LocalClient()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	status, err := lc.Status(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	type Peer struct {
		Name           string   `json:"name"`
		IPs            []string `json:"ips"`
		Online         bool     `json:"online"`
		OS             string   `json:"os"`
		ExitNodeOption bool     `json:"exitNodeOption"`
	}
	peers := []Peer{}
	for _, p := range status.Peer {
		ips := []string{}
		for _, ip := range p.TailscaleIPs {
			ips = append(ips, ip.String())
		}
		peers = append(peers, Peer{
			Name:           strings.TrimSuffix(p.HostName, "."),
			IPs:            ips,
			Online:         p.Online,
			OS:             p.OS,
			ExitNodeOption: p.ExitNodeOption,
		})
	}
	writeJSON(w, http.StatusOK, peers)
}

func (a *App) handleAbout(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	tsConnected := a.tsServer != nil
	tabCount := len(a.tabs)
	exitNode := a.config.ExitNode
	restoreTabs := a.config.RestoreTabs
	autoConnect := a.config.AutoConnect
	a.mu.Unlock()

	info := map[string]interface{}{
		"name":                 appName,
		"version":              version,
		"commit":               commit,
		"go":                   runtime.Version(),
		"platform":             runtime.GOOS + "/" + runtime.GOARCH,
		"webview2":             "System WebView2 runtime",
		"tailscale":            moduleVersion("tailscale.com"),
		"tsnet":                moduleVersion("tailscale.com/tsnet"),
		"configDir":            configDir(),
		"webviewData":          webviewDataPath(),
		"socks":                socksAddr,
		"localAPI":             fmt.Sprintf("127.0.0.1:%d", a.serverPort),
		"tsConnected":          tsConnected,
		"exitNode":             exitNode,
		"tabCount":             tabCount,
		"restoreTabs":          restoreTabs,
		"autoConnect":          autoConnect,
		"sshHostKeyPolicy":     "TOFU pinned in encrypted config",
		"webviewProxyMode":     "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
		"downloadInterception": "explicit <a download> links plus in-app API",
		"cookieBackend":        "WebView2 SQLite cookie store",
		"logPath":              logFilePath(),
	}
	json.NewEncoder(w).Encode(info)
}

func moduleVersion(path string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if bi.Main.Path == path {
		return bi.Main.Version
	}
	for _, dep := range bi.Deps {
		if dep.Path == path {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "bundled"
}

func (a *App) handleSSHConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Host string `json:"host"`
		User string `json:"user"`
		Port int    `json:"port"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	body.Host = strings.TrimSpace(body.Host)
	body.User = strings.TrimSpace(body.User)
	if body.Host == "" {
		httpError(w, 400, "Missing host", "")
		return
	}
	if err := validateCustomURLHost("ssh", body.Host); err != nil {
		httpError(w, 400, "Invalid host", err.Error())
		return
	}
	if body.User == "" {
		body.User = "root"
	}
	if err := validateCustomURLUser("ssh", body.User); err != nil {
		httpError(w, 400, "Invalid user", err.Error())
		return
	}
	if body.Port == 0 {
		body.Port = 22
	}
	if body.Port <= 0 || body.Port > 65535 {
		httpError(w, 400, "Invalid port", "")
		return
	}
	body.Cols = clampTerminalDimension(body.Cols, minTerminalCols, maxTerminalCols)
	if body.Cols == 0 {
		body.Cols = 80
	}
	body.Rows = clampTerminalDimension(body.Rows, minTerminalRows, maxTerminalRows)
	if body.Rows == 0 {
		body.Rows = 24
	}

	ts := a.ts()
	if ts == nil {
		httpError(w, 503, "Tailscale not connected", "")
		return
	}
	addr := net.JoinHostPort(body.Host, fmt.Sprintf("%d", body.Port))
	conn, err := ts.Dial(context.Background(), "tcp", addr)
	if err != nil {
		httpError(w, 500, "Connection failed", err.Error())
		return
	}

	authMethods := a.getSSHAuthMethods(body.Host)
	config := &ssh.ClientConfig{
		User:            body.User,
		Auth:            authMethods,
		HostKeyCallback: a.hostKeyCallback(body.Host),
		Timeout:         10 * time.Second,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		conn2, err2 := ts.Dial(context.Background(), "tcp", addr)
		if err2 != nil {
			httpError(w, 500, "Connection failed", err2.Error())
			return
		}
		noneConfig := &ssh.ClientConfig{
			User: body.User, Auth: []ssh.AuthMethod{ssh.Password("")},
			HostKeyCallback: a.hostKeyCallback(body.Host), Timeout: 5 * time.Second,
		}
		_, _, _, _ = ssh.NewClientConn(conn2, addr, noneConfig)
		conn2.Close()
		tabID := fmt.Sprintf("term-%s-%d", body.Host, time.Now().UnixMilli())
		a.mu.Lock()
		a.pendingAuth[tabID] = &pendingSSH{host: body.Host, user: body.User, port: body.Port, cols: body.Cols, rows: body.Rows}
		a.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]interface{}{"tabId": tabID, "needsPassword": true})
		return
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	tabID := fmt.Sprintf("term-%s-%d", body.Host, time.Now().UnixMilli())
	a.mu.Lock()
	a.sshConns[tabID] = client
	a.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"tabId": tabID, "status": "connected"})
}

func (a *App) handleSSHResize(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }

func (a *App) hostKeyCallback(host string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		keyData := string(ssh.MarshalAuthorizedKey(key))
		fingerprint := ssh.FingerprintSHA256(key)

		a.mu.Lock()
		conf := a.config.Hosts[host]
		stored := conf.HostKey
		if stored == "" {
			conf.HostKey = keyData
			conf.HostKeyFingerprint = fingerprint
			a.config.Hosts[host] = conf
			cfg := a.config
			a.mu.Unlock()
			return SaveConfig(cfg)
		}
		a.mu.Unlock()

		storedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(stored))
		if err != nil {
			return fmt.Errorf("stored SSH host key for %s is invalid: %w", host, err)
		}
		if !bytes.Equal(storedKey.Marshal(), key.Marshal()) {
			return fmt.Errorf("SSH host key mismatch for %s: expected %s, got %s", host, ssh.FingerprintSHA256(storedKey), fingerprint)
		}
		return nil
	}
}

func (a *App) handleSSHAuthPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		TabId    string `json:"tabId"`
		Password string `json:"password"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	body.TabId = strings.TrimSpace(body.TabId)
	if body.TabId == "" {
		httpError(w, 400, "Missing tabId", "")
		return
	}
	a.mu.Lock()
	pending := a.pendingAuth[body.TabId]
	delete(a.pendingAuth, body.TabId)
	a.mu.Unlock()
	if pending == nil {
		httpError(w, 404, "No pending auth", "")
		return
	}
	ts := a.ts()
	if ts == nil {
		httpError(w, 503, "Tailscale not connected", "")
		return
	}
	addr := net.JoinHostPort(pending.host, fmt.Sprintf("%d", pending.port))
	conn, err := ts.Dial(context.Background(), "tcp", addr)
	if err != nil {
		httpError(w, 500, "Connection failed", err.Error())
		return
	}
	config := &ssh.ClientConfig{
		User: pending.user,
		Auth: []ssh.AuthMethod{
			ssh.Password(body.Password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = body.Password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: a.hostKeyCallback(pending.host), Timeout: 10 * time.Second,
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		httpError(w, 401, "Authentication failed", err.Error())
		return
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	a.mu.Lock()
	a.sshConns[body.TabId] = client
	a.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]string{"status": "connected", "tabId": body.TabId})
}
