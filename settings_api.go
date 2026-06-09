package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHKeyEntry stored in credential manager
type SSHKeyEntry struct {
	Name        string   `json:"name"`
	Fingerprint string   `json:"fingerprint"`
	Hosts       []string `json:"hosts"`
}

func (a *App) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings/unlock-method", a.authMiddleware(a.handleSetUnlockMethod))
	mux.HandleFunc("/api/settings/master-password", a.authMiddleware(a.handleSetMasterPassword))
	mux.HandleFunc("/api/settings/tailscale-key", a.authMiddleware(a.handleSetTailscaleKey))
	mux.HandleFunc("/api/settings/tailscale-disconnect", a.authMiddleware(a.handleTailscaleDisconnect))
	mux.HandleFunc("/api/settings/ssh-keys", a.authMiddleware(a.handleSSHKeys))
	mux.HandleFunc("/api/settings/ssh-keys/", a.authMiddleware(a.handleSSHKeys))
	mux.HandleFunc("/api/settings/ssh-keys/generate", a.authMiddleware(a.handleGenerateSSHKey))
	mux.HandleFunc("/api/settings/hosts", a.authMiddleware(a.handleHosts))
	mux.HandleFunc("/api/settings/hosts/", a.authMiddleware(a.handleHosts))
	mux.HandleFunc("/api/settings/appearance", a.authMiddleware(a.handleAppearance))
	mux.HandleFunc("/api/settings/exit-node", a.authMiddleware(a.handleExitNode))
	mux.HandleFunc("/api/settings/save-tabs", a.authMiddleware(a.handleSaveTabs))
	mux.HandleFunc("/api/settings/config", a.authMiddleware(a.handleGetConfig))
}

func (a *App) handleSetUnlockMethod(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Method string `json:"method"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	a.config.UnlockMethod = body.Method
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleSetMasterPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	// Store PBKDF2 hash of the master password
	hash := sha256.Sum256([]byte(body.Password + "womprat-salt"))
	SaveCredential("master-hash", fmt.Sprintf("%x", hash))
	a.config.UnlockMethod = "master"
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleSetTailscaleKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if err := SaveCredential("tailscale-key", body.Key); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Restart tailscale with new key
	if err := a.startTailscale(); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "saved", "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "connected"})
}

func (a *App) handleTailscaleDisconnect(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	ts := a.tsServer
	a.tsServer = nil
	a.mu.Unlock()
	if ts != nil {
		ts.Close()
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
}

func (a *App) handleSSHKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		keys := a.listSSHKeys()
		json.NewEncoder(w).Encode(keys)
	case "POST":
		var body struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if err := a.importSSHKey(body.Name, body.Content); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	case "DELETE":
		// DELETE /api/settings/ssh-keys/<name>
		parts := strings.Split(r.URL.Path, "/")
		name := parts[len(parts)-1]
		DeleteCredential("ssh-key/" + name)
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	}
}

func (a *App) handleGenerateSSHKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	privBlock, err := ssh.MarshalPrivateKey(priv, body.Name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	privPEM := pem.EncodeToMemory(privBlock)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	SaveCredential("ssh-key/"+body.Name, string(privPEM))
	fingerprint := ssh.FingerprintSHA256(sshPub)

	json.NewEncoder(w).Encode(map[string]string{
		"status":      "ok",
		"name":        body.Name,
		"fingerprint": fingerprint,
		"publicKey":   string(ssh.MarshalAuthorizedKey(sshPub)),
	})
}

func (a *App) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(a.config.Hosts)
	case "PATCH":
		parts := strings.Split(r.URL.Path, "/")
		host := parts[len(parts)-1]
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		conf := a.config.Hosts[host]
		if v, ok := body["user"].(string); ok {
			conf.User = v
		}
		if v, ok := body["port"].(float64); ok {
			conf.Port = int(v)
		}
		if v, ok := body["keyName"].(string); ok {
			conf.KeyName = v
		}
		if v, ok := body["nickname"].(string); ok {
			conf.Nickname = v
		}
		a.config.Hosts[host] = conf
		SaveConfig(a.config)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func (a *App) handleAppearance(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		FontSize    int    `json:"fontSize"`
		Theme       string `json:"theme"`
		RestoreTabs bool   `json:"restoreTabs"`
		AutoConnect bool   `json:"autoConnect"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	a.config.FontSize = body.FontSize
	a.config.Theme = body.Theme
	a.config.RestoreTabs = body.RestoreTabs
	a.config.AutoConnect = body.AutoConnect
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(a.config)
}

// Helper: list SSH keys from credential store
func (a *App) listSSHKeys() []SSHKeyEntry {
	entries := []SSHKeyEntry{}
	keyDir := filepath.Join(configDir(), "creds", "ssh-key")
	files, err := os.ReadDir(keyDir)
	if err != nil {
		return entries
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		keyData, err := GetCredential("ssh-key/" + name)
		if err != nil {
			continue
		}
		hosts := []string{}
		for host, hc := range a.config.Hosts {
			if hc.KeyName == name {
				hosts = append(hosts, host)
			}
		}
		entries = append(entries, SSHKeyEntry{Name: name, Fingerprint: fingerprintFromPEM(keyData), Hosts: hosts})
	}
	return entries
}

// Helper: import an SSH key from PEM content
func (a *App) importSSHKey(name, content string) error {
	// Validate it's a valid key
	_, err := ssh.ParseRawPrivateKey([]byte(content))
	if err != nil {
		return fmt.Errorf("invalid SSH key: %w", err)
	}
	return SaveCredential("ssh-key/"+name, content)
}

// Helper: get fingerprint from PEM key data
func fingerprintFromPEM(pemData string) string {
	priv, err := ssh.ParseRawPrivateKey([]byte(pemData))
	if err != nil {
		return "—"
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return "—"
	}
	return ssh.FingerprintSHA256(signer.PublicKey())
}

func (a *App) handleExitNode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		a.mu.Lock()
		exitNode := a.config.ExitNode
		a.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]string{"exitNode": exitNode})
	case "POST":
		var body struct {
			ExitNode string `json:"exitNode"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		// Toggle proxy routing mode. Actual Tailscale exit-node selection is still
		// handled by tsnet/control state; this flag only decides whether the local
		// SOCKS proxy sends public destinations to tsnet or direct.
		a.mu.Lock()
		a.config.ExitNode = body.ExitNode
		useExitNode = body.ExitNode != ""
		a.mu.Unlock()
		SaveConfig(a.config)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "exitNode": body.ExitNode})
	}
}

func (a *App) handleSaveTabs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Tabs []SavedTab `json:"tabs"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	a.config.OpenTabs = body.Tabs
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
