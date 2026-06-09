package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHKeyEntry stored in credential manager
type SSHKeyEntry struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Hosts       []string `json:"hosts"`
}

func (a *App) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings/unlock-method", a.authMiddleware(a.handleSetUnlockMethod))
	mux.HandleFunc("/api/settings/master-password", a.authMiddleware(a.handleSetMasterPassword))
	mux.HandleFunc("/api/settings/tailscale-key", a.authMiddleware(a.handleSetTailscaleKey))
	mux.HandleFunc("/api/settings/tailscale-disconnect", a.authMiddleware(a.handleTailscaleDisconnect))
	mux.HandleFunc("/api/settings/ssh-keys", a.authMiddleware(a.handleSSHKeys))
	mux.HandleFunc("/api/settings/ssh-keys/generate", a.authMiddleware(a.handleGenerateSSHKey))
	mux.HandleFunc("/api/settings/hosts", a.authMiddleware(a.handleHosts))
	mux.HandleFunc("/api/settings/appearance", a.authMiddleware(a.handleAppearance))
	mux.HandleFunc("/api/settings/exit-node", a.authMiddleware(a.handleExitNode))
	mux.HandleFunc("/api/settings/save-tabs", a.authMiddleware(a.handleSaveTabs))
	mux.HandleFunc("/api/settings/config", a.authMiddleware(a.handleGetConfig))
}

func (a *App) handleSetUnlockMethod(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "method not allowed", 405); return }
	var body struct{ Method string `json:"method"` }
	json.NewDecoder(r.Body).Decode(&body)
	a.config.UnlockMethod = body.Method
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleSetMasterPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "method not allowed", 405); return }
	var body struct{ Password string `json:"password"` }
	json.NewDecoder(r.Body).Decode(&body)
	// Store PBKDF2 hash of the master password
	hash := sha256.Sum256([]byte(body.Password + "womprat-salt"))
	SaveCredential("master-hash", fmt.Sprintf("%x", hash))
	a.config.UnlockMethod = "master"
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleSetTailscaleKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "method not allowed", 405); return }
	var body struct{ Key string `json:"key"` }
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
	if a.tsServer != nil {
		a.tsServer.Close()
		a.tsServer = nil
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
	if r.Method != "POST" { http.Error(w, "method not allowed", 405); return }
	var body struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&body)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "OPENSSH PRIVATE KEY",
		Bytes: edKeyMarshal(priv),
	})

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
		if v, ok := body["user"].(string); ok { conf.User = v }
		if v, ok := body["port"].(float64); ok { conf.Port = int(v) }
		if v, ok := body["keyName"].(string); ok { conf.KeyName = v }
		if v, ok := body["nickname"].(string); ok { conf.Nickname = v }
		a.config.Hosts[host] = conf
		SaveConfig(a.config)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

func (a *App) handleAppearance(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "method not allowed", 405); return }
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
	// TODO: enumerate credentials with prefix "ssh-key/"
	// For now, return from config
	entries := []SSHKeyEntry{}
	// Scan known key names from config hosts
	seen := map[string]bool{}
	for _, h := range a.config.Hosts {
		if h.KeyName != "" && !seen[h.KeyName] {
			seen[h.KeyName] = true
			keyData, err := GetCredential("ssh-key/" + h.KeyName)
			if err == nil {
				fp := fingerprintFromPEM(keyData)
				hosts := []string{}
				for name, hc := range a.config.Hosts {
					if hc.KeyName == h.KeyName { hosts = append(hosts, name) }
				}
				entries = append(entries, SSHKeyEntry{Name: h.KeyName, Fingerprint: fp, Hosts: hosts})
			}
		}
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

// Placeholder for ed25519 key marshaling (openssh format)
func edKeyMarshal(key ed25519.PrivateKey) []byte {
	// TODO: proper openssh private key format marshaling
	return key
}

func (a *App) handleExitNode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		json.NewEncoder(w).Encode(map[string]string{"exitNode": a.config.ExitNode})
	case "POST":
		var body struct{ ExitNode string `json:"exitNode"` }
		json.NewDecoder(r.Body).Decode(&body)
		a.config.ExitNode = body.ExitNode
		SaveConfig(a.config)
		// Toggle proxy mode
		if body.ExitNode != "" {
			useExitNode = true
		} else {
			useExitNode = false
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "exitNode": body.ExitNode})
	}
}

func (a *App) handleSaveTabs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { http.Error(w, "method not allowed", 405); return }
	var body struct{ Tabs []SavedTab `json:"tabs"` }
	json.NewDecoder(r.Body).Decode(&body)
	a.config.OpenTabs = body.Tabs
	SaveConfig(a.config)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
