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

	"github.com/jchv/go-webview2"
		"golang.org/x/crypto/ssh"
	"tailscale.com/tsnet"
)

//go:embed frontend/*
var frontendFS embed.FS

var useExitNode = false // when true, proxy routes ALL traffic (internet via exit node)

const (
	appName    = "womprat"
	)

// Tab represents either a terminal (SSH) or browser tab
type Tab struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // "terminal" or "browser"
	Title   string `json:"title"`
	URL     string `json:"url,omitempty"`     // for browser tabs
	Host    string `json:"host,omitempty"`    // for terminal tabs
	SSHUser string `json:"sshUser,omitempty"` // for terminal tabs
}

type pendingSSH struct {
	host string
	user string
	port int
	cols int
	rows int
}

type App struct {
	mu           sync.Mutex
	config       *AppConfig
	tsServer     *tsnet.Server
	tabs         []Tab
	sshConns     map[string]*ssh.Client
	pendingAuth  map[string]*pendingSSH
	sessionToken string // random token, required on all API requests
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

	// Start local HTTP server for the UI
	mux := http.NewServeMux()
	app.registerRoutes(mux)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	go http.Serve(listener, mux)

	// Start SOCKS5 proxy immediately (port must be open before WebView2 starts)
	startSOCKS5Listener(app)

	// Start Tailscale
	if err := app.startTailscale(); err != nil {
		log.Printf("Tailscale start failed: %v (will prompt for key)", err)
	}


	// Open WebView2 window
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
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

	// Set dark title bar on Windows
	applyDarkMode(w)
	w.Navigate(url)
	w.Run()
}

func (a *App) startTailscale() error {
	authKey, err := GetCredential("tailscale-key")
	if err != nil || authKey == "" {
		return fmt.Errorf("no tailscale auth key in keychain")
	}

	a.tsServer = &tsnet.Server{
		Hostname:  appName,
		AuthKey:   authKey,
		Dir:       tsnetStateDir(),
		Ephemeral: false, // persist node identity for stable MagicDNS name
	}

	_, err = a.tsServer.Up(context.Background())
	return err
}

func tsnetStateDir() string {
	dir, _ := os.UserConfigDir()
	return fmt.Sprintf("%s/%s/tsnet", dir, appName)
}

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *App) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check X-Session-Token header or ?token= query param
		token := r.Header.Get("X-Session-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != a.sessionToken {
			httpError(w, 403, "Forbidden", "Invalid or missing session token")
			return
		}
		next(w, r)
	}
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	// Serve frontend (injects token)
	mux.HandleFunc("/", a.serveFrontend)

	// Settings API
	a.registerSettingsRoutes(mux)

	// Browser settings
	a.registerBrowserRoutes(mux)



	// Downloads
	a.registerDownloadRoutes(mux)

	// Reverse proxy for browser tabs (routes through tsnet)


	// API endpoints
	mux.HandleFunc("/api/auth/status", a.authMiddleware(a.handleAuthStatus))
	mux.HandleFunc("/api/auth/save-key", a.authMiddleware(a.handleSaveKey))
	mux.HandleFunc("/api/tailscale/status", a.authMiddleware(a.handleTSStatus))
	mux.HandleFunc("/api/tailscale/peers", a.authMiddleware(a.handleTSPeers))
	mux.HandleFunc("/api/ssh/connect", a.authMiddleware(a.handleSSHConnect))
	mux.HandleFunc("/api/ssh/resize", a.authMiddleware(a.handleSSHResize))
	// WebSocket for terminal I/O
	mux.HandleFunc("/api/ssh/ws", a.authMiddleware(a.handleSSHWebSocketFull))
	mux.HandleFunc("/api/ssh/auth-password", a.authMiddleware(a.handleSSHAuthPassword))
}

func (a *App) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	_, err := GetCredential("tailscale-key")
	status := map[string]interface{}{
		"hasKey":    err == nil,
		"connected": a.tsServer != nil,
	}
	json.NewEncoder(w).Encode(status)
}

func (a *App) handleSaveKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := SaveCredential("tailscale-key", body.Key); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Try to start tailscale with the new key
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if body.Port == 0 {
		body.Port = 22
	}

	// Connect via tsnet
	addr := fmt.Sprintf("%s:%d", body.Host, body.Port)
	conn, err := a.tsServer.Dial(context.Background(), "tcp", addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("dial failed: %v", err), 500)
		return
	}

	// Try key-based auth first
	authMethods := a.getSSHAuthMethods(body.Host)
	
	config := &ssh.ClientConfig{
		User:            body.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		// If key auth failed, check if we should try password
		conn.Close()
		// Try reconnecting to check if password auth is available
		conn2, err2 := a.tsServer.Dial(context.Background(), "tcp", addr)
		if err2 != nil {
			httpError(w, 500, "Connection failed", err2.Error())
			return
		}
		// Check if server supports password auth by attempting with none
		noneConfig := &ssh.ClientConfig{
			User:            body.User,
			Auth:            []ssh.AuthMethod{ssh.Password("")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		}
		_, _, _, noneErr := ssh.NewClientConn(conn2, addr, noneConfig)
		conn2.Close()
		if noneErr != nil {
			// Server likely supports keyboard-interactive or password
			tabID := fmt.Sprintf("term-%s-%d", body.Host, time.Now().UnixMilli())
			a.mu.Lock()
			a.pendingAuth[tabID] = &pendingSSH{host: body.Host, user: body.User, port: body.Port, cols: body.Cols, rows: body.Rows}
			a.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]interface{}{"tabId": tabID, "needsPassword": true})
			return
		}
		httpError(w, 500, "SSH authentication failed", err.Error())
		return
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	tabID := fmt.Sprintf("term-%s-%d", body.Host, time.Now().UnixMilli())

	a.mu.Lock()
	a.sshConns[tabID] = client
	a.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"tabId": tabID, "status": "connected"})
}

func (a *App) handleSSHResize(w http.ResponseWriter, r *http.Request) {
	// Placeholder for terminal resize
	w.WriteHeader(200)
}

// Utility to copy between reader/writer (for terminal I/O)

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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
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

func (a *App) serveFrontend(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" || path == "" {
		path = "frontend/index.html"
	} else if strings.HasPrefix(path, "/vendor/") || strings.HasPrefix(path, "/fonts/") {
		path = "frontend" + path
	} else {
		path = "frontend" + path
	}

	data, err := frontendFS.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Inject session token into HTML pages
	if strings.HasSuffix(path, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		content := strings.Replace(string(data), "</head>",
			fmt.Sprintf(`<script>window.__SESSION_TOKEN="%s";</script></head>`, a.sessionToken), 1)
		w.Write([]byte(content))
		return
	}

	// Serve other files normally
	if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".mjs") {
		w.Header().Set("Content-Type", "application/javascript")
	} else if strings.HasSuffix(path, ".css") {
		w.Header().Set("Content-Type", "text/css")
	} else if strings.HasSuffix(path, ".ttf") {
		w.Header().Set("Content-Type", "font/ttf")
	}
	w.Write(data)
}
