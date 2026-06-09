package main

import (
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
	Type  string `json:"type"` // "terminal", "browser", "settings"
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
	if body.Port == 0 {
		body.Port = 22
	}

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
				for i := range answers {
					answers[i] = body.Password
				}
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

// chromeOverlayJS returns JS that injects native-sized browser chrome into external pages.
func chromeOverlayJS(port int, token string) string {
	return fmt.Sprintf(`
(function() {
  // Don't inject into our own shell pages.
  if (location.hostname === '127.0.0.1' && location.port === '%d') return;
  if (window.__wompratChromeInstalled) return;
  window.__wompratChromeInstalled = true;

  function install() {
    if (!document.documentElement || !document.body) {
      requestAnimationFrame(install);
      return;
    }

    const style = document.createElement('style');
    style.textContent = %s;
    (document.head || document.documentElement).appendChild(style);

    const icons = {
      back: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M12.73 4.22a.75.75 0 0 1 .05 1.06L8.56 10l4.22 4.72a.75.75 0 1 1-1.12 1L7 10.53a.75.75 0 0 1 0-1.06l4.66-5.2a.75.75 0 0 1 1.07-.05Z"/></svg>',
      forward: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M7.27 4.22a.75.75 0 0 0-.05 1.06L11.44 10l-4.22 4.72a.75.75 0 1 0 1.12 1L13 10.53a.75.75 0 0 0 0-1.06l-4.66-5.2a.75.75 0 0 0-1.07-.05Z"/></svg>',
      reload: '<svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path d="M15.65 6.35A6.5 6.5 0 1 0 16.5 10a.75.75 0 0 1 1.5 0 8 8 0 1 1-2.34-5.66V3.25a.75.75 0 0 1 1.5 0v3.5c0 .41-.34.75-.75.75h-3.5a.75.75 0 0 1 0-1.5h2.74Z"/></svg>',
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
        <button id="womprat-go">Go</button>
      </div>`+"`"+`;
    document.body.appendChild(bar);

    const input = document.getElementById('womprat-url');
    input.value = location.href;

    function navigateFromInput() {
      let u = input.value.trim();
      if (!u) return;
      if (!/^https?:\/\//i.test(u)) u = 'http://' + u;
      womprat_navigate(u);
    }

    document.getElementById('womprat-back').addEventListener('click', () => history.back());
    document.getElementById('womprat-forward').addEventListener('click', () => history.forward());
    document.getElementById('womprat-reload').addEventListener('click', () => location.reload());
    document.getElementById('womprat-go').addEventListener('click', navigateFromInput);
    document.getElementById('womprat-home').addEventListener('click', () => womprat_goHome());
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') navigateFromInput(); });

    async function refresh() {
      try {
        const state = JSON.parse(await womprat_getTabs());
        const tabs = document.getElementById('womprat-tabs');
        tabs.textContent = '';
        state.tabs.forEach((t) => {
          const item = document.createElement('button');
          item.className = 'wt' + (t.id === state.activeTab ? ' active' : '');
          item.textContent = (t.title || t.url || t.host || 'tab').slice(0, 24);
          item.addEventListener('click', () => womprat_switchTab(t.id));
          tabs.appendChild(item);
        });
        if (document.activeElement !== input) input.value = location.href;
      } catch (e) {}
    }

    refresh();
    setInterval(refresh, 1500);
  }
  install();
})();
`, port, chromeOverlayCSS)
}

var chromeOverlayCSS = "`" + `
#womprat-chrome{position:fixed!important;top:0!important;left:0!important;right:0!important;height:84px!important;
  background:rgba(32,32,32,.97)!important;backdrop-filter:blur(10px)!important;display:flex!important;
  flex-direction:column!important;z-index:2147483647!important;font:14px/1.2 'Segoe UI Variable','Segoe UI',system-ui,sans-serif!important;
  color:#f3f3f3!important;border-bottom:1px solid rgba(255,255,255,.10)!important;box-sizing:border-box!important}
#womprat-chrome *,#womprat-chrome *::before,#womprat-chrome *::after{box-sizing:border-box!important;font-family:'Segoe UI Variable','Segoe UI',system-ui,sans-serif!important}
#womprat-tab-row{height:40px!important;display:flex!important;align-items:center!important;gap:6px!important;padding:4px 8px!important;min-width:0!important}
#womprat-tabs{display:flex!important;align-items:center!important;gap:4px!important;min-width:0!important;overflow:hidden!important;flex:1!important}
#womprat-chrome .wt{height:32px!important;max-width:220px!important;padding:0 12px!important;border:1px solid transparent!important;border-radius:4px!important;
  background:transparent!important;color:#cfcfcf!important;cursor:pointer!important;opacity:.85!important;font-size:14px!important;line-height:30px!important;
  white-space:nowrap!important;overflow:hidden!important;text-overflow:ellipsis!important;flex:0 1 auto!important}
#womprat-chrome .wt:hover{background:rgba(255,255,255,.08)!important;color:#fff!important}
#womprat-chrome .wt.active{background:rgba(255,255,255,.12)!important;color:#fff!important;font-weight:600!important}
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
html{scroll-padding-top:84px!important}
body{padding-top:84px!important;margin-top:0!important}
` + "`"
