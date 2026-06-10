package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/crypto/ssh"
	"tailscale.com/ipn"
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

const maxSettingsJSONBody = 1 << 20

func decodeSettingsJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeSettingsJSONInternal(w, r, dst, false)
}

func decodeOptionalSettingsJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeSettingsJSONInternal(w, r, dst, true)
}

func decodeSettingsJSONInternal(w http.ResponseWriter, r *http.Request, dst any, allowEmpty bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if allowEmpty && err == io.EOF {
			return true
		}
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "invalid JSON: multiple JSON values", http.StatusBadRequest)
		return false
	}
	return true
}

func resourceNameFromPath(path, prefix string) (string, error) {
	if !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("invalid path")
	}
	name := strings.TrimPrefix(path, prefix)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("missing resource name")
	}
	return name, nil
}

func (a *App) handleSetUnlockMethod(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Method string `json:"method"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	if body.Method != "master" && body.Method != "dpapi" {
		http.Error(w, "unsupported unlock method", 400)
		return
	}
	a.mu.Lock()
	a.config.UnlockMethod = body.Method
	cfg := a.config
	a.mu.Unlock()
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
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
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	if body.Password == "" {
		http.Error(w, "empty password", 400)
		return
	}
	salt := make([]byte, masterSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	hash := pbkdf2.Key([]byte(body.Password), salt, masterKDFIterations, masterHashBytes, sha256.New)
	record := masterHashRecord{
		KDF:        masterKDF,
		Iterations: masterKDFIterations,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Hash:       base64.StdEncoding.EncodeToString(hash),
	}
	data, err := json.Marshal(record)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := SaveCredential("master-hash", string(data)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.mu.Lock()
	a.config.UnlockMethod = "master"
	cfg := a.config
	a.mu.Unlock()
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "kdf": masterKDF})
}

func (a *App) handleSetTailscaleKey(w http.ResponseWriter, r *http.Request) {
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
	if strings.TrimSpace(body.Key) == "" {
		http.Error(w, "empty tailscale key", 400)
		return
	}
	if err := SaveCredential("tailscale-key", strings.TrimSpace(body.Key)); err != nil {
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
		if !decodeSettingsJSON(w, r, &body) {
			return
		}
		if err := a.importSSHKey(body.Name, body.Content); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	case "DELETE":
		namePart, err := resourceNameFromPath(r.URL.Path, "/api/settings/ssh-keys/")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		name, err := safeSSHKeyName(namePart)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := DeleteCredential("ssh-key/" + name); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	default:
		http.Error(w, "method not allowed", 405)
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
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	name, err := safeSSHKeyName(body.Name)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	privBlock, err := ssh.MarshalPrivateKey(priv, name)
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

	if err := SaveCredential("ssh-key/"+name, string(privPEM)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fingerprint := ssh.FingerprintSHA256(sshPub)

	json.NewEncoder(w).Encode(map[string]string{
		"status":      "ok",
		"name":        name,
		"fingerprint": fingerprint,
		"publicKey":   string(ssh.MarshalAuthorizedKey(sshPub)),
	})
}

func (a *App) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		a.mu.Lock()
		hosts := a.config.Hosts
		a.mu.Unlock()
		json.NewEncoder(w).Encode(hosts)
	case "PATCH":
		host, err := resourceNameFromPath(r.URL.Path, "/api/settings/hosts/")
		if err != nil || validateCustomURLHost("ssh", host) != nil {
			http.Error(w, "invalid host", 400)
			return
		}
		var body struct {
			User     *string `json:"user,omitempty"`
			Port     *int    `json:"port,omitempty"`
			KeyName  *string `json:"keyName,omitempty"`
			Nickname *string `json:"nickname,omitempty"`
			URL      *string `json:"url,omitempty"`
		}
		if !decodeSettingsJSON(w, r, &body) {
			return
		}
		a.mu.Lock()
		conf := a.config.Hosts[host]
		if body.User != nil {
			conf.User = strings.TrimSpace(*body.User)
		}
		if body.Port != nil {
			if *body.Port <= 0 || *body.Port > 65535 {
				a.mu.Unlock()
				http.Error(w, "invalid port", 400)
				return
			}
			conf.Port = *body.Port
		}
		if body.KeyName != nil {
			keyName := strings.TrimSpace(*body.KeyName)
			if keyName != "" {
				if _, err := safeSSHKeyName(keyName); err != nil {
					a.mu.Unlock()
					http.Error(w, err.Error(), 400)
					return
				}
			}
			conf.KeyName = keyName
		}
		if body.Nickname != nil {
			conf.Nickname = strings.TrimSpace(*body.Nickname)
		}
		if body.URL != nil {
			conf.URL = sanitizeHostConfigURL(*body.URL)
		}
		a.config.Hosts[host] = conf
		cfg := a.config
		a.mu.Unlock()
		if err := SaveConfig(cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", 405)
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
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	a.mu.Lock()
	a.config.FontSize = normalizeFontSize(body.FontSize)
	a.config.Theme = normalizeTheme(body.Theme)
	a.config.RestoreTabs = body.RestoreTabs
	a.config.AutoConnect = body.AutoConnect
	cfg := a.config
	a.mu.Unlock()
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (a *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	cfg := a.config
	a.mu.Unlock()
	json.NewEncoder(w).Encode(cfg)
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
		a.mu.Lock()
		for host, hc := range a.config.Hosts {
			if hc.KeyName == name {
				hosts = append(hosts, host)
			}
		}
		a.mu.Unlock()
		entries = append(entries, SSHKeyEntry{Name: name, Fingerprint: fingerprintFromPEM(keyData), Hosts: hosts})
	}
	return entries
}

// Helper: import an SSH key from PEM content
func (a *App) importSSHKey(name, content string) error {
	name, err := safeSSHKeyName(name)
	if err != nil {
		return err
	}
	// Validate it's a valid key
	_, err = ssh.ParseRawPrivateKey([]byte(content))
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
		if !decodeSettingsJSON(w, r, &body) {
			return
		}
		if err := a.applyExitNodePreference(r.Context(), body.ExitNode); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		a.mu.Lock()
		a.config.ExitNode = body.ExitNode
		useExitNode = body.ExitNode != ""
		cfg := a.config
		a.mu.Unlock()
		if err := SaveConfig(cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "exitNode": body.ExitNode})
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (a *App) applyExitNodePreference(ctx context.Context, exitNode string) error {
	ts := a.ts()
	if ts == nil {
		if exitNode == "" {
			return nil
		}
		return fmt.Errorf("tailscale not connected")
	}
	lc, err := ts.LocalClient()
	if err != nil {
		return err
	}
	prefs := &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			RouteAll: exitNode != "",
		},
		RouteAllSet:   true,
		ExitNodeIPSet: true,
		ExitNodeIDSet: true,
	}
	if exitNode != "" {
		status, err := lc.Status(ctx)
		if err != nil {
			return err
		}
		for _, p := range status.Peer {
			name := strings.TrimSuffix(p.HostName, ".")
			if name == exitNode || p.DNSName == exitNode || strings.TrimSuffix(p.DNSName, ".") == exitNode {
				if !p.ExitNodeOption {
					return fmt.Errorf("%s is not advertised as an exit node", exitNode)
				}
				if len(p.TailscaleIPs) == 0 {
					return fmt.Errorf("exit node %s has no tailscale IP", exitNode)
				}
				prefs.ExitNodeIP = p.TailscaleIPs[0]
				break
			}
		}
		if !prefs.ExitNodeIP.IsValid() {
			return fmt.Errorf("exit node %s was not found in tailnet peers", exitNode)
		}
	}
	_, err = lc.EditPrefs(ctx, prefs)
	return err
}

func (a *App) handleSaveTabs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Tabs []SavedTab `json:"tabs"`
	}
	if !decodeSettingsJSON(w, r, &body) {
		return
	}
	sanitizedTabs := sanitizeSavedTabs(body.Tabs)
	a.mu.Lock()
	a.config.OpenTabs = sanitizedTabs
	cfg := a.config
	a.mu.Unlock()
	if err := SaveConfig(cfg); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
