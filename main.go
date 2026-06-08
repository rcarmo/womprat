package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/jchv/go-webview2"
	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh"
	"tailscale.com/tsnet"
)

//go:embed frontend/*
var frontendFS embed.FS

const (
	appName    = "womprat"
	keyService = "womprat-tailscale"
	keyUser    = "authkey"
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

type App struct {
	mu       sync.Mutex
	tsServer *tsnet.Server
	tabs     []Tab
	sshConns map[string]*ssh.Client
}

func main() {
	app := &App{
		sshConns: make(map[string]*ssh.Client),
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

	// Start Tailscale
	if err := app.startTailscale(); err != nil {
		log.Printf("Tailscale start failed: %v (will prompt for key)", err)
	}

	// Open WebView2 window
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
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
	w.Navigate(url)
	w.Run()
}

func (a *App) startTailscale() error {
	authKey, err := keyring.Get(keyService, keyUser)
	if err != nil || authKey == "" {
		return fmt.Errorf("no tailscale auth key in keychain")
	}

	a.tsServer = &tsnet.Server{
		Hostname: appName,
		AuthKey:  authKey,
		Dir:      tsnetStateDir(),
	}

	_, err = a.tsServer.Up(context.Background())
	return err
}

func tsnetStateDir() string {
	dir, _ := os.UserConfigDir()
	return fmt.Sprintf("%s/%s/tsnet", dir, appName)
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	// Serve frontend
	mux.Handle("/", http.FileServer(http.FS(frontendFS)))

	// API endpoints
	mux.HandleFunc("/api/auth/status", a.handleAuthStatus)
	mux.HandleFunc("/api/auth/save-key", a.handleSaveKey)
	mux.HandleFunc("/api/tailscale/status", a.handleTSStatus)
	mux.HandleFunc("/api/tailscale/peers", a.handleTSPeers)
	mux.HandleFunc("/api/ssh/connect", a.handleSSHConnect)
	mux.HandleFunc("/api/ssh/resize", a.handleSSHResize)
	// WebSocket for terminal I/O
	mux.HandleFunc("/api/ssh/ws", a.handleSSHWebSocket)
}

func (a *App) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	_, err := keyring.Get(keyService, keyUser)
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
	if err := keyring.Set(keyService, keyUser, body.Key); err != nil {
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
	status, err := a.tsServer.LocalClient().Status(r.Context())
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
	status, err := a.tsServer.LocalClient().Status(r.Context())
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

	// SSH connection (agent auth or key-based)
	config := &ssh.ClientConfig{
		User:            body.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeysCallback(sshAgentAuth)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		conn.Close()
		http.Error(w, fmt.Sprintf("ssh handshake failed: %v", err), 500)
		return
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	tabID := fmt.Sprintf("term-%s-%d", body.Host, len(a.sshConns))

	a.mu.Lock()
	a.sshConns[tabID] = client
	a.mu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"tabId": tabID, "status": "connected"})
}

func (a *App) handleSSHResize(w http.ResponseWriter, r *http.Request) {
	// Placeholder for terminal resize
	w.WriteHeader(200)
}

func (a *App) handleSSHWebSocket(w http.ResponseWriter, r *http.Request) {
	// WebSocket upgrade + PTY I/O will be implemented with nhooyr.io/websocket
	http.Error(w, "websocket handler - TODO", 501)
}

func sshAgentAuth() ([]ssh.Signer, error) {
	// Try to use SSH agent
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		return nil, fmt.Errorf("no SSH agent")
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, err
	}
	_ = conn // TODO: implement agent protocol
	return nil, fmt.Errorf("agent not implemented yet")
}

// Placeholder for reading SSH keys from ~/.ssh/
func readSSHKeys() []ssh.Signer {
	return nil
}

// Utility to copy between reader/writer (for terminal I/O)
func ioCopy(dst io.Writer, src io.Reader) {
	io.Copy(dst, src)
}
