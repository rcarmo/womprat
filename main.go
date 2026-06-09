package main

import (
	"context"
	"embed"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
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

type pendingSSH struct {
	host string
	user string
	port int
	cols int
	rows int
}

type Tab struct {
	ID    string `json:"id"`
	Type  string `json:"type"`  // "terminal", "browser", "settings"
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Host  string `json:"host,omitempty"`
	User  string `json:"user,omitempty"`
	Port  int    `json:"port,omitempty"`
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
	}

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

	// Start Tailscale
	if err := app.startTailscale(); err != nil {
		log.Printf("Tailscale start failed: %v (will prompt for key)", err)
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

	// Set dark title bar
	applyDarkMode(w)

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

	w.Bind("womprat_navigate", func(url string) {
		app.navigateBrowser(url)
	})

	w.Bind("womprat_switchTab", func(tabID string) {
		app.switchTab(tabID)
	})

	w.Bind("womprat_closeTab", func(tabID string) {
		app.closeTab(tabID)
	})

	w.Bind("womprat_newBrowser", func(url string) {
		app.newBrowserTab(url)
	})

	w.Bind("womprat_newTerminal", func(host, user string, port int) {
		app.newTerminalTab(host, user, port)
	})

	w.Bind("womprat_goHome", func() {
		app.goHome()
	})

	// Inject floating chrome overlay into every page
	w.Init(chromeOverlayJS(app.serverPort, app.sessionToken))

	// Navigate to shell
	w.Navigate(shellURL)

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
	a.webview.Navigate(url)
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
		shellURL := fmt.Sprintf("http://127.0.0.1:%d/?tab=%s", a.serverPort, tabID)
		a.webview.Navigate(shellURL)
	}
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
	a.tabs = append(a.tabs, Tab{ID: tabID, Type: "terminal", Title: host, Host: host, User: user, Port: port})
	a.activeTab = tabID
	a.mu.Unlock()
	shellURL := fmt.Sprintf("http://127.0.0.1:%d/?tab=%s", a.serverPort, tabID)
	a.webview.Navigate(shellURL)
}

func (a *App) goHome() {
	a.mu.Lock()
	a.activeTab = ""
	a.mu.Unlock()
	shellURL := fmt.Sprintf("http://127.0.0.1:%d/", a.serverPort)
	a.webview.Navigate(shellURL)
}

func (a *App) startTailscale() error {
	authKey, err := GetCredential("tailscale-key")
	if err != nil || authKey == "" {
		return fmt.Errorf("no tailscale auth key")
	}
	a.tsServer = &tsnet.Server{
		Hostname:  appName,
		AuthKey:   authKey,
		Dir:       tsnetStateDir(),
		Ephemeral: false,
	}
	_, err = a.tsServer.Up(context.Background())
	return err
}

func tsnetStateDir() string {
	dir, _ := os.UserConfigDir()
	return fmt.Sprintf("%s/%s/tsnet", dir, appName)
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", a.serveFrontend)
	mux.HandleFunc("/api/auth/status", a.authMiddleware(a.handleAuthStatus))
	mux.HandleFunc("/api/auth/save-key", a.authMiddleware(a.handleSaveKey))
	mux.HandleFunc("/api/tailscale/status", a.authMiddleware(a.handleTSStatus))
	mux.HandleFunc("/api/tailscale/peers", a.authMiddleware(a.handleTSPeers))
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
		next(w, r)
	}
}

func (a *App) serveFrontend(w http.ResponseWriter, r *http.Request) {
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

func (a *App) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	_, err := GetCredential("tailscale-key")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hasKey":    err == nil,
		"connected": a.tsServer != nil,
	})
}

func (a *App) handleSaveKey(w http.ResponseWriter, r *http.Request) {
	var body struct{ Key string `json:"key"` }
	json.NewDecoder(r.Body).Decode(&body)
	SaveCredential("tailscale-key", body.Key)
	if err := a.startTailscale(); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "saved", "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "connected"})
}

func (a *App) handleTSStatus(w http.ResponseWriter, r *http.Request) {
	if a.tsServer == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
		return
	}
	lc, _ := a.tsServer.LocalClient()
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
	if a.tsServer == nil {
		json.NewEncoder(w).Encode([]string{})
		return
	}
	lc, _ := a.tsServer.LocalClient()
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

func (a *App) handleSSHConnect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host string `json:"host"`
		User string `json:"user"`
		Port int    `json:"port"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.Port == 0 { body.Port = 22 }

	addr := fmt.Sprintf("%s:%d", body.Host, body.Port)
	conn, err := a.tsServer.Dial(context.Background(), "tcp", addr)
	if err != nil {
		httpError(w, 500, "Connection failed", err.Error())
		return
	}

	authMethods := a.getSSHAuthMethods(body.Host)
	config := &ssh.ClientConfig{
		User:            body.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		conn2, err2 := a.tsServer.Dial(context.Background(), "tcp", addr)
		if err2 != nil {
			httpError(w, 500, "Connection failed", err2.Error())
			return
		}
		noneConfig := &ssh.ClientConfig{
			User: body.User, Auth: []ssh.AuthMethod{ssh.Password("")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second,
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
	addr := fmt.Sprintf("%s:%d", pending.host, pending.port)
	conn, err := a.tsServer.Dial(context.Background(), "tcp", addr)
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
				for i := range answers { answers[i] = body.Password }
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second,
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

// chromeOverlayJS returns JS that injects a floating tab bar into every page
func chromeOverlayJS(port int, token string) string {
	return fmt.Sprintf(`
(function() {
  // Don't inject into our own shell pages
  if (location.hostname === '127.0.0.1' && location.port === '%d') return;
  
  const STYLE = document.createElement('style');
  STYLE.textContent = %s;
  document.head.appendChild(STYLE);
  
  const BAR = document.createElement('div');
  BAR.id = 'womprat-chrome';
  BAR.innerHTML = '<div id="womprat-tabs"></div><button id="womprat-home" title="Home">⌂</button>';
  document.body.appendChild(BAR);
  
  async function refresh() {
    const state = JSON.parse(await womprat_getTabs());
    const tabs = document.getElementById('womprat-tabs');
    tabs.innerHTML = state.tabs.map(t => 
      '<span class="wt ' + (t.id===state.activeTab?'active':'') + '" onclick="womprat_switchTab(\'' + t.id + '\')">' +
      t.title.slice(0,20) + '</span>'
    ).join('');
  }
  
  document.getElementById('womprat-home').onclick = () => womprat_goHome();
  refresh();
  setInterval(refresh, 2000);
})();
`, port, chromeOverlayCSS)
}

var chromeOverlayCSS = "`" + `
#womprat-chrome{position:fixed;top:0;left:0;right:0;height:28px;background:rgba(32,32,32,.95);
  backdrop-filter:blur(8px);display:flex;align-items:center;padding:0 8px;gap:4px;z-index:999999;
  font:12px/1 'Segoe UI Variable',system-ui,sans-serif;color:#fff;border-bottom:1px solid rgba(255,255,255,.08)}
#womprat-chrome .wt{padding:4px 10px;border-radius:4px;cursor:pointer;opacity:.6;font-size:11px}
#womprat-chrome .wt:hover{opacity:.8;background:rgba(255,255,255,.06)}
#womprat-chrome .wt.active{opacity:1;background:rgba(255,255,255,.1);font-weight:600}
#womprat-chrome #womprat-home{background:none;border:none;color:#fff;cursor:pointer;margin-left:auto;
  font-size:14px;opacity:.6;padding:4px 8px;border-radius:4px}
#womprat-chrome #womprat-home:hover{opacity:1;background:rgba(255,255,255,.06)}
body{margin-top:28px !important}
` + "`"

