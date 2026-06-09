package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/crypto/ssh"
	"tailscale.com/tsnet"
)

//go:embed frontend/*
var frontendFS embed.FS

var useExitNode = false

const appName = "womprat"

var (
	version = "0.1.0"
	commit  = "dev"
)

type pendingSSH struct {
	host string
	user string
	port int
	cols int
	rows int
}

type Tab struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // "terminal", "browser", "settings"
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`
	Favicon string `json:"favicon,omitempty"`
	Host    string `json:"host,omitempty"`
	User    string `json:"user,omitempty"`
	Port    int    `json:"port,omitempty"`
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
	webview      webview2.WebView
	serverPort   int
}

func main() {
	cfg, _ := LoadConfig()
	token := generateSessionToken()
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
	go http.Serve(listener, mux)

	// Start SOCKS5 proxy (port must be open before WebView2 starts)
	startSOCKS5Listener(app)

	// Start Tailscale only after the configured unlock gate is satisfied.
	if !app.locked {
		if err := app.startTailscale(); err != nil {
			log.Printf("Tailscale start failed: %v (will prompt for key)", err)
		}
	}

	// Create WebView2
	shellURL := fmt.Sprintf("http://127.0.0.1:%d/", app.serverPort)

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     true,
		AutoFocus: true,
		DataPath:  webviewDataPath(),
		WindowOptions: webview2.WindowOptions{
			Title:  "womprat",
			Width:  1200,
			Height: 800,
		},
	})
	if w == nil {
		log.Fatal("Failed to create WebView2 window")
	}
	defer w.Destroy()
	app.webview = w

	// Set native title bar appearance
	applyDarkMode(w)
	applyAppIcon(w)

	// Bind Go functions callable from JS in any page
	w.Bind("womprat_getTabs", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		data, _ := json.Marshal(map[string]interface{}{
			"tabs":      app.tabs,
			"activeTab": app.activeTab,
			"port":      app.serverPort,
			"token":     app.sessionToken,
		})
		return string(data)
	})

	w.Bind("womprat_getNetworkState", func() string {
		app.mu.Lock()
		defer app.mu.Unlock()
		data, _ := json.Marshal(map[string]interface{}{
			"exitNode":   app.config.ExitNode,
			"exitActive": useExitNode,
		})
		return string(data)
	})

	w.Bind("womprat_navigate", func(url string) {
		app.navigateBrowser(url)
	})

	w.Bind("womprat_switchTab", func(tabID string) {
		app.switchTab(tabID)
	})

	w.Bind("womprat_updateTitle", func(title, url, favicon string) {
		app.updateActiveBrowserTitle(title, url, favicon)
	})

	w.Bind("womprat_closeTab", func(tabID string) {
		app.closeTab(tabID)
	})

	w.Bind("womprat_forgetTab", func(tabID string) {
		app.forgetTab(tabID)
	})

	w.Bind("womprat_newBrowser", func(url string) {
		app.newBrowserTab(url)
	})

	w.Bind("womprat_newTerminal", func(host, user string, port int) {
		app.newTerminalTab(host, user, port)
	})

	w.Bind("womprat_registerLocalTab", func(tabJSON string) {
		app.registerLocalTab(tabJSON)
	})

	w.Bind("womprat_goHome", func() {
		app.goHome()
	})

	// Inject floating chrome overlay into every page
	w.Init(chromeOverlayJS(app.serverPort, app.sessionToken))

	// Navigate to shell with a cache-buster so stale WebView2 shell HTML/JS
	// cannot resurrect removed iframe code paths.
	w.Navigate(fmt.Sprintf("%s?v=%d", shellURL, time.Now().UnixMilli()))

	w.Run()
}

func (a *App) navigateBrowser(url string) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	// Update active tab URL
	a.mu.Lock()
	for i, t := range a.tabs {
		if t.ID == a.activeTab && t.Type == "browser" {
			a.tabs[i].URL = url
		}
	}
	a.mu.Unlock()
	a.persistOpenTabs()
	a.webview.Navigate(url)
}

func (a *App) updateActiveBrowserTitle(title, url, favicon string) {
	title = strings.TrimSpace(title)
	url = strings.TrimSpace(url)
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
	_ = SaveConfig(cfg)
}

func (a *App) switchTab(tabID string) {
	a.mu.Lock()
	var tab *Tab
	for i := range a.tabs {
		if a.tabs[i].ID == tabID {
			tab = &a.tabs[i]
			break
		}
	}
	a.activeTab = tabID
	a.mu.Unlock()

	if tab == nil {
		return
	}

	switch tab.Type {
	case "browser":
		a.webview.Navigate(tab.URL)
	case "terminal", "settings":
		shellURL := fmt.Sprintf("http://127.0.0.1:%d/?tab=%s&v=%d", a.serverPort, tabID, time.Now().UnixMilli())
		a.webview.Navigate(shellURL)
	}
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
	a.persistOpenTabs()
}

func (a *App) closeTab(tabID string) {
	a.mu.Lock()
	newTabs := []Tab{}
	for _, t := range a.tabs {
		if t.ID != tabID {
			newTabs = append(newTabs, t)
		}
	}
	a.tabs = newTabs
	if a.activeTab == tabID && len(newTabs) > 0 {
		a.activeTab = newTabs[0].ID
	}
	a.mu.Unlock()
	a.persistOpenTabs()

	if len(newTabs) == 0 {
		a.goHome()
	} else {
		a.switchTab(a.activeTab)
	}
}

func (a *App) newBrowserTab(url string) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	tabID := fmt.Sprintf("browser-%d", time.Now().UnixMilli())
	a.mu.Lock()
	a.tabs = append(a.tabs, Tab{ID: tabID, Type: "browser", Title: url, URL: url})
	a.activeTab = tabID
	a.mu.Unlock()
	a.persistOpenTabs()
	a.webview.Navigate(url)
}

func (a *App) newTerminalTab(host, user string, port int) {
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	tabID := fmt.Sprintf("term-%d", time.Now().UnixMilli())
	a.mu.Lock()
	a.tabs = upsertTab(a.tabs, Tab{ID: tabID, Type: "terminal", Title: host, Host: host, User: user, Port: port})
	a.activeTab = tabID
	a.mu.Unlock()
	a.persistOpenTabs()
	shellURL := fmt.Sprintf("http://127.0.0.1:%d/?tab=%s&v=%d", a.serverPort, tabID, time.Now().UnixMilli())
	a.webview.Navigate(shellURL)
}

func (a *App) registerLocalTab(tabJSON string) {
	var tab Tab
	if err := json.Unmarshal([]byte(tabJSON), &tab); err != nil || tab.ID == "" || tab.Type == "" {
		return
	}
	if tab.Title == "" {
		tab.Title = tab.URL
		if tab.Title == "" {
			tab.Title = tab.Host
		}
	}
	a.mu.Lock()
	a.tabs = upsertTab(a.tabs, tab)
	a.activeTab = tab.ID
	a.mu.Unlock()
	a.persistOpenTabs()
}

func upsertTab(tabs []Tab, tab Tab) []Tab {
	for i := range tabs {
		if tabs[i].ID == tab.ID {
			tabs[i] = tab
			return tabs
		}
	}
	return append(tabs, tab)
}

func (a *App) goHome() {
	a.mu.Lock()
	a.activeTab = ""
	a.mu.Unlock()
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
	mux.HandleFunc("/api/ssh/connect", a.authMiddleware(a.handleSSHConnect))
	mux.HandleFunc("/api/ssh/resize", a.authMiddleware(a.handleSSHResize))
	mux.HandleFunc("/api/ssh/ws", a.authMiddleware(a.handleSSHWebSocketFull))
	mux.HandleFunc("/api/ssh/auth-password", a.authMiddleware(a.handleSSHAuthPassword))
}

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
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

func (a *App) serveFrontend(w http.ResponseWriter, r *http.Request) {
	// The shell changes rapidly during development and must never come from
	// WebView2's persistent HTTP cache; stale shell JS can resurrect old iframe
	// code paths and trigger frame-ancestors CSP failures.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	path := r.URL.Path
	if path == "/" || path == "" {
		path = "frontend/index.html"
	} else {
		path = "frontend" + path
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
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob:; connect-src 'self' ws: http://127.0.0.1:*; frame-src 'self' http://127.0.0.1:*; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline' 'unsafe-eval'")
		content := strings.Replace(string(data), "</head>",
			fmt.Sprintf(`<script>window.__SESSION_TOKEN="%s";window.__PORT=%d;</script></head>`, a.sessionToken, a.serverPort), 1)
		w.Write([]byte(content))
		return
	}
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".mjs") {
		w.Header().Set("Content-Type", "application/javascript")
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
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hasKey":       err == nil,
		"connected":    a.ts() != nil,
		"locked":       a.isLocked(),
		"unlockMethod": a.config.UnlockMethod,
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
	json.NewDecoder(r.Body).Decode(&body)
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
	var body struct {
		Key string `json:"key"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	SaveCredential("tailscale-key", body.Key)
	if err := a.startTailscale(); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "saved", "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "connected"})
}

func (a *App) handleTSStatus(w http.ResponseWriter, r *http.Request) {
	ts := a.ts()
	if ts == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
		return
	}
	lc, _ := ts.LocalClient()
	status, err := lc.Status(r.Context())
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "connected",
		"hostname": status.Self.HostName,
		"ip":       status.TailscaleIPs,
	})
}

func (a *App) handleTSPeers(w http.ResponseWriter, r *http.Request) {
	ts := a.ts()
	if ts == nil {
		json.NewEncoder(w).Encode([]string{})
		return
	}
	lc, _ := ts.LocalClient()
	status, err := lc.Status(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	type Peer struct {
		Name   string   `json:"name"`
		IPs    []string `json:"ips"`
		Online bool     `json:"online"`
		OS     string   `json:"os"`
	}
	peers := []Peer{}
	for _, p := range status.Peer {
		ips := []string{}
		for _, ip := range p.TailscaleIPs {
			ips = append(ips, ip.String())
		}
		peers = append(peers, Peer{
			Name:   strings.TrimSuffix(p.HostName, "."),
			IPs:    ips,
			Online: p.Online,
			OS:     p.OS,
		})
	}
	json.NewEncoder(w).Encode(peers)
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
	var body struct {
		Host string `json:"host"`
		User string `json:"user"`
		Port int    `json:"port"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Port == 0 {
		body.Port = 22
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
	var body struct {
		TabId    string `json:"tabId"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)
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

// chromeOverlayJS returns JS that injects native-sized browser chrome into external pages.
func chromeOverlayJS(port int, token string) string {
	return fmt.Sprintf(`
(function() {
  // Inject chrome only into the top-level browser document. Third-party pages
  // often contain ad/login/sandbox iframes; adding womprat chrome inside those
  // frames creates nested URL bars and breaks page layout.
  if (window.top !== window.self) return;
  // Don't inject into our own shell pages.
  if (location.hostname === '127.0.0.1' && location.port === '%d') return;
  if (window.__wompratChromeInstalled) return;
  window.__wompratChromeInstalled = true;

  function install() {
    if (!document.documentElement) {
      requestAnimationFrame(install);
      return;
    }

    const root = document.body || document.documentElement;
    const style = document.createElement('style');
    style.textContent = %s;
    (document.head || document.documentElement).appendChild(style);

    const icons = {
      back: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M9.78 4.22a.75.75 0 0 1 0 1.06L5.56 9.5h10.69a.75.75 0 0 1 0 1.5H5.56l4.22 4.22a.75.75 0 1 1-1.06 1.06l-5.5-5.5a.75.75 0 0 1 0-1.06l5.5-5.5a.75.75 0 0 1 1.06 0Z"/></svg>',
      forward: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M10.22 4.22a.75.75 0 0 1 1.06 0l5.5 5.5a.75.75 0 0 1 0 1.06l-5.5 5.5a.75.75 0 1 1-1.06-1.06L14.44 11H3.75a.75.75 0 0 1 0-1.5h10.69l-4.22-4.22a.75.75 0 0 1 0-1.06Z"/></svg>',
      reload: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M15.65 6.35A6.5 6.5 0 1 0 16.5 10a.75.75 0 0 1 1.5 0 8 8 0 1 1-2.34-5.66V3.25a.75.75 0 0 1 1.5 0v3.5c0 .41-.34.75-.75.75h-3.5a.75.75 0 0 1 0-1.5h2.74Z"/></svg>',
      close: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M4.22 4.22a.75.75 0 0 1 1.06 0L10 8.94l4.72-4.72a.75.75 0 1 1 1.06 1.06L11.06 10l4.72 4.72a.75.75 0 1 1-1.06 1.06L10 11.06l-4.72 4.72a.75.75 0 0 1-1.06-1.06L8.94 10 4.22 5.28a.75.75 0 0 1 0-1.06Z"/></svg>',
      home: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M9.28 2.78a1 1 0 0 1 1.44 0l6.5 6.78a.75.75 0 0 1-1.08 1.04l-.64-.67V16a2 2 0 0 1-2 2h-2.25a.75.75 0 0 1-.75-.75V13h-1v4.25a.75.75 0 0 1-.75.75H6.5a2 2 0 0 1-2-2V9.93l-.64.67a.75.75 0 1 1-1.08-1.04l6.5-6.78Z"/></svg>'
    };
    const i = (name) => '<span class="womprat-icon">' + icons[name] + '</span>';
    const bar = document.createElement('div');
    bar.id = 'womprat-chrome';
    bar.innerHTML = `+"`"+`
      <div id="womprat-tab-row">
        <div id="womprat-tabs"></div>
        <button id="womprat-home" title="Home" aria-label="Home">${i('home')}</button>
      </div>
      <div id="womprat-url-row">
        <button id="womprat-back" title="Back" aria-label="Back">${i('back')}</button>
        <button id="womprat-forward" title="Forward" aria-label="Forward">${i('forward')}</button>
        <button id="womprat-reload" title="Reload" aria-label="Reload">${i('reload')}</button>
        <input id="womprat-url" spellcheck="false">
        <span id="womprat-route" title="Exit node route status"></span>
        <button id="womprat-go">Go</button>
      </div>`+"`"+`;
    root.appendChild(bar);

    const input = document.getElementById('womprat-url');
    input.value = location.href;

    function installContentZoomControls() {
      let contentZoom = Number(sessionStorage.getItem('wompratContentZoom') || '1') || 1;
      function apply() {
        contentZoom = Math.max(0.5, Math.min(3, contentZoom));
        sessionStorage.setItem('wompratContentZoom', String(contentZoom));
        document.documentElement.style.setProperty('--womprat-content-zoom', String(contentZoom));
      }
      function change(delta) {
        contentZoom = Math.round((contentZoom + delta) * 100) / 100;
        apply();
      }
      apply();

      // In Chromium/WebView2, trackpad pinch is delivered to pages as Ctrl+wheel.
      // Prevent WebView page zoom so the app chrome remains native-sized, then
      // apply zoom only to page content children outside #womprat-chrome.
      window.addEventListener('wheel', (e) => {
        if (!e.ctrlKey) return;
        e.preventDefault();
        e.stopPropagation();
        change(e.deltaY < 0 ? 0.1 : -0.1);
      }, { capture: true, passive: false });

      document.addEventListener('keydown', (e) => {
        const key = e.key;
        const ctrl = e.ctrlKey || e.metaKey;
        if (!ctrl) return;
        if (key === '+' || key === '=') { e.preventDefault(); change(0.1); }
        else if (key === '-' || key === '_') { e.preventDefault(); change(-0.1); }
        else if (key === '0') { e.preventDefault(); contentZoom = 1; apply(); }
      }, true);
    }

    function navigateFromInput() {
      let u = input.value.trim();
      if (!u) return;
      const sshMatch = u.match(/^ssh:\/\/(?:([^@]+)@)?([^:\/]+)(?::(\d+))?/i);
      if (sshMatch) {
        const user = sshMatch[1] || 'root';
        const host = sshMatch[2];
        const port = sshMatch[3] ? parseInt(sshMatch[3], 10) : 22;
        if (window.womprat_newTerminal) womprat_newTerminal(host, user, port);
        return;
      }
      if (!/^https?:\/\//i.test(u)) u = 'http://' + u;
      updateRoutePill(true);
      womprat_navigate(u);
    }

    function currentFavicon() {
      const selectors = [
        'link[rel~="icon"][href]',
        'link[rel="shortcut icon"][href]',
        'link[rel="apple-touch-icon"][href]',
        'link[rel="mask-icon"][href]'
      ];
      for (const sel of selectors) {
        const el = document.querySelector(sel);
        if (el?.href) return new URL(el.getAttribute('href'), location.href).href;
      }
      return new URL('/favicon.ico', location.origin).href;
    }

    function reportPageTitle() {
      const title = (document.title || location.hostname || location.href || '').trim();
      if (window.womprat_updateTitle) womprat_updateTitle(title, location.href, currentFavicon());
    }

    function installTitleReporter() {
      reportPageTitle();
      window.addEventListener('load', reportPageTitle);
      document.addEventListener('readystatechange', reportPageTitle);
      const titleEl = document.querySelector('title');
      if (titleEl) new MutationObserver(reportPageTitle).observe(titleEl, { childList: true, subtree: true, characterData: true });
      new MutationObserver(reportPageTitle).observe(document.documentElement, { childList: true, subtree: true });
      setInterval(reportPageTitle, 1500);
    }

    function installDownloadInterceptor() {
      document.addEventListener('click', (e) => {
        const a = e.target?.closest?.('a[href][download]');
        if (!a) return;
        const href = new URL(a.getAttribute('href'), location.href).href;
        if (!/^https?:\/\//i.test(href)) return;
        e.preventDefault();
        fetch('http://127.0.0.1:%d/api/download?token=%s&url=' + encodeURIComponent(href)).catch(() => {});
      }, true);
    }

    async function updateRoutePill(loading) {
      const pill = document.getElementById('womprat-route');
      if (!pill) return;
      try {
        const ns = window.womprat_getNetworkState ? JSON.parse(await womprat_getNetworkState()) : {};
        const isLoading = loading || document.readyState !== 'complete';
        if (ns.exitActive && ns.exitNode) {
          pill.textContent = ns.exitNode;
          pill.title = isLoading ? 'Loading via exit node ' + ns.exitNode : 'Traffic is routed via exit node ' + ns.exitNode;
          pill.className = 'active' + (isLoading ? ' loading' : '');
        } else {
          pill.textContent = '';
          pill.className = '';
        }
      } catch(e) {
        pill.textContent = '';
        pill.className = '';
      }
    }

    document.getElementById('womprat-back').addEventListener('click', () => history.back());
    document.getElementById('womprat-forward').addEventListener('click', () => history.forward());
    document.getElementById('womprat-reload').addEventListener('click', () => location.reload());
    document.getElementById('womprat-go').addEventListener('click', navigateFromInput);
    document.getElementById('womprat-home').addEventListener('click', () => womprat_goHome());
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') navigateFromInput(); });

    async function currentTabState() {
      try { return JSON.parse(await womprat_getTabs()); } catch(e) { return { tabs: [], activeTab: '' }; }
    }
    async function switchRelativeTab(delta) {
      const state = await currentTabState();
      const tabs = state.tabs || [];
      if (!tabs.length) return;
      const idx = Math.max(0, tabs.findIndex(t => t.id === state.activeTab));
      const next = tabs[(idx + delta + tabs.length) %% tabs.length];
      if (next) womprat_switchTab(next.id);
    }
    async function switchTabAt(index) {
      const state = await currentTabState();
      const tabs = state.tabs || [];
      if (!tabs.length) return;
      const target = tabs[Math.min(index, tabs.length - 1)];
      if (target) womprat_switchTab(target.id);
    }
    async function closeActiveTab() {
      const state = await currentTabState();
      if (state.activeTab) womprat_closeTab(state.activeTab);
      else womprat_goHome();
    }
    function focusAddress(selectAll) {
      input.focus();
      if (selectAll) input.select();
    }
    document.addEventListener('keydown', (e) => {
      const key = e.key.toLowerCase();
      const ctrl = e.ctrlKey || e.metaKey;
      if (e.altKey && !ctrl && key === 'arrowleft') { e.preventDefault(); history.back(); return; }
      if (e.altKey && !ctrl && key === 'arrowright') { e.preventDefault(); history.forward(); return; }
      if (key === 'f5' || (ctrl && key === 'r')) { e.preventDefault(); location.reload(); return; }
      if ((ctrl && key === 'l') || (e.altKey && !ctrl && key === 'd')) { e.preventDefault(); focusAddress(true); return; }
      if (ctrl && key === 't') { e.preventDefault(); womprat_goHome(); return; }
      if (ctrl && key === 'w') { e.preventDefault(); closeActiveTab(); return; }
      if (ctrl && (key === 'tab' || key === 'pagedown')) { e.preventDefault(); switchRelativeTab(e.shiftKey ? -1 : 1); return; }
      if (ctrl && key === 'pageup') { e.preventDefault(); switchRelativeTab(-1); return; }
      if (ctrl && /^[1-9]$/.test(key)) { e.preventDefault(); switchTabAt(key === '9' ? 8 : Number(key) - 1); return; }
      if (e.altKey && !ctrl && key === 'home') { e.preventDefault(); womprat_goHome(); return; }
    }, true);

    async function refresh() {
      try {
        const state = JSON.parse(await womprat_getTabs());
        const tabs = document.getElementById('womprat-tabs');
        tabs.textContent = '';
        state.tabs.forEach((t) => {
          const item = document.createElement('div');
          item.className = 'wt' + (t.id === state.activeTab ? ' active' : '');
          const title = document.createElement('button');
          title.className = 'wt-title';
          if (t.favicon) {
            const fav = document.createElement('img');
            fav.className = 'wt-favicon';
            fav.src = t.favicon;
            fav.alt = '';
            fav.referrerPolicy = 'no-referrer';
            title.appendChild(fav);
          }
          const label = document.createElement('span');
          label.textContent = (t.title || t.url || t.host || 'tab').slice(0, 24);
          title.appendChild(label);
          title.addEventListener('click', () => womprat_switchTab(t.id));
          const close = document.createElement('button');
          close.className = 'wt-close';
          close.title = 'Close tab';
          close.setAttribute('aria-label', 'Close tab');
          close.innerHTML = i('close');
          close.addEventListener('click', (e) => { e.stopPropagation(); womprat_closeTab(t.id); });
          item.appendChild(title);
          item.appendChild(close);
          tabs.appendChild(item);
        });
        if (document.activeElement !== input) input.value = location.href;
      } catch (e) {}
    }

    installContentZoomControls();
    installTitleReporter();
    installDownloadInterceptor();
    updateRoutePill(true);
    document.addEventListener('readystatechange', () => updateRoutePill(false));
    window.addEventListener('load', () => updateRoutePill(false));
    refresh();
    setInterval(refresh, 1500);
    setInterval(() => updateRoutePill(false), 1500);
  }
  install();
})();
`, port, chromeOverlayCSS, port, token)
}

var chromeOverlayCSS = "`" + `
:root{--womprat-chrome-offset:84px;--womprat-content-zoom:1}
#womprat-chrome{position:fixed!important;top:0!important;left:0!important;right:0!important;width:auto!important;height:84px!important;
  transform:none!important;transform-origin:top left!important;
  background:rgba(32,32,32,.97)!important;backdrop-filter:blur(10px)!important;display:flex!important;
  flex-direction:column!important;z-index:2147483647!important;font:14px/1.2 'Segoe UI Variable','Segoe UI',system-ui,sans-serif!important;
  color:#f3f3f3!important;border-bottom:1px solid rgba(255,255,255,.10)!important;box-sizing:border-box!important}
#womprat-chrome *,#womprat-chrome *::before,#womprat-chrome *::after{box-sizing:border-box!important;font-family:'Segoe UI Variable','Segoe UI',system-ui,sans-serif!important}
#womprat-tab-row{height:40px!important;display:flex!important;align-items:center!important;gap:6px!important;padding:4px 8px!important;min-width:0!important}
#womprat-tabs{display:flex!important;align-items:center!important;gap:4px!important;min-width:0!important;overflow:hidden!important;flex:1!important}
#womprat-chrome .wt{height:32px!important;max-width:240px!important;padding:0 4px 0 10px!important;border:1px solid transparent!important;border-radius:4px!important;
  background:transparent!important;color:#cfcfcf!important;cursor:pointer!important;opacity:.85!important;font-size:14px!important;line-height:30px!important;
  white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important;flex:0 1 auto!important;display:inline-flex!important;align-items:center!important;gap:4px!important;min-width:0!important}
#womprat-chrome .wt:hover{background:rgba(255,255,255,.08)!important;color:#fff!important}
#womprat-chrome .wt.active{background:rgba(255,255,255,.12)!important;color:#fff!important;font-weight:600!important}
#womprat-chrome .wt-title{height:30px!important;min-width:0!important;max-width:190px!important;flex:1 1 auto!important;padding:0!important;border:0!important;background:transparent!important;color:inherit!important;justify-content:flex-start!important;overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important;font-weight:inherit!important;gap:6px!important}
#womprat-chrome .wt-title:hover{background:transparent!important;color:inherit!important}
#womprat-chrome .wt-title span{overflow:hidden!important;text-overflow:ellipsis!important;white-space:nowrap!important}
#womprat-chrome .wt-favicon{width:16px!important;height:16px!important;min-width:16px!important;object-fit:contain!important;border-radius:2px!important}
#womprat-chrome .wt-close{width:24px!important;height:24px!important;min-width:24px!important;padding:0!important;border:0!important;border-radius:4px!important;background:transparent!important;color:#cfcfcf!important;opacity:.65!important;flex:0 0 24px!important}
#womprat-chrome .wt-close:hover{background:rgba(255,255,255,.10)!important;color:#fff!important;opacity:1!important}
#womprat-chrome .wt-close .womprat-icon{width:16px!important;height:16px!important;flex-basis:16px!important}
#womprat-url-row{height:44px!important;display:flex!important;align-items:center!important;gap:6px!important;padding:6px 8px!important;min-width:0!important}
#womprat-chrome button{height:32px!important;min-width:32px!important;border:1px solid transparent!important;border-radius:4px!important;background:transparent!important;
  color:#d6d6d6!important;display:inline-flex!important;align-items:center!important;justify-content:center!important;cursor:pointer!important;font-size:14px!important;padding:0 10px!important}
#womprat-chrome button:hover{background:rgba(255,255,255,.08)!important;color:#fff!important}
#womprat-chrome .womprat-icon{width:20px!important;height:20px!important;display:inline-block!important;flex:0 0 20px!important;color:currentColor!important}
#womprat-chrome .womprat-icon svg{width:100%!important;height:100%!important;display:block!important}
#womprat-chrome #womprat-back,#womprat-chrome #womprat-forward,#womprat-chrome #womprat-reload{padding:0!important;line-height:32px!important}
#womprat-chrome #womprat-home{margin-left:auto!important;flex:0 0 auto!important}
#womprat-chrome #womprat-url{height:32px!important;min-width:0!important;flex:1 1 auto!important;border:1px solid rgba(255,255,255,.16)!important;
  border-radius:4px!important;background:rgba(255,255,255,.06)!important;color:#f3f3f3!important;padding:0 10px!important;font-size:14px!important;
  line-height:32px!important;outline:none!important;color-scheme:dark!important}
#womprat-chrome #womprat-url:focus{border-color:#60cdff!important;background:rgba(0,0,0,.28)!important}
#womprat-chrome #womprat-route{display:none!important;align-items:center!important;height:22px!important;max-width:150px!important;padding:0 7px!important;border-radius:999px!important;
  border:1px solid rgba(255,255,255,.10)!important;background:rgba(255,255,255,.05)!important;color:rgba(255,255,255,.55)!important;
  font-size:11px!important;font-weight:600!important;letter-spacing:0!important;text-transform:none!important;white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important;flex:0 1 auto!important}
#womprat-chrome #womprat-route.active{display:inline-flex!important}
#womprat-chrome #womprat-route.loading::after{content:""!important;width:6px!important;height:6px!important;border-radius:50%!important;background:currentColor!important;margin-left:6px!important;animation:wompratPulse 1s infinite ease-in-out!important}
@keyframes wompratPulse{0%,100%{opacity:.35;transform:scale(.85)}50%{opacity:1;transform:scale(1.15)}}
html{height:100%!important;overflow:hidden!important;scroll-padding-top:var(--womprat-chrome-offset)!important}
body{height:calc(100vh - var(--womprat-chrome-offset))!important;overflow:auto!important;margin-top:var(--womprat-chrome-offset)!important;padding-top:0!important;box-sizing:border-box!important}
body > :not(#womprat-chrome){zoom:var(--womprat-content-zoom)!important}
` + "`"
