package main

// config.go — self-contained settings management
// All settings stored in %APPDATA%/womprat/config.enc (DPAPI-encrypted JSON)
// Secrets in Windows Credential Manager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AppConfig holds all non-secret settings (DPAPI-encrypted on disk)
type AppConfig struct {
	// Unlock method: "master" (password) or "dpapi" (transparent)
	UnlockMethod string `json:"unlockMethod"`

	// Window state
	WindowWidth  int  `json:"windowWidth"`
	WindowHeight int  `json:"windowHeight"`
	WindowX      int  `json:"windowX"`
	WindowY      int  `json:"windowY"`
	Maximized    bool `json:"maximized"`

	// Session restore
	OpenTabs []SavedTab `json:"openTabs"`

	// Per-host SSH settings
	Hosts map[string]HostConfig `json:"hosts"`

	// Browser
	SavePasswords bool `json:"savePasswords"`

	// Networking
	ExitNode string `json:"exitNode"` // tailnet peer to use as exit node (empty = direct)

	// Appearance
	FontSize    int    `json:"fontSize"`
	Theme       string `json:"theme"`
	RestoreTabs bool   `json:"restoreTabs"`
	AutoConnect bool   `json:"autoConnect"`

	// Diagnostics
	DebugLog bool `json:"debugLog"` // write runtime log file and enable WebView dev tooling
}

type SavedTab struct {
	Type    string `json:"type"` // "terminal" or "browser"
	Title   string `json:"title"`
	Host    string `json:"host,omitempty"`
	User    string `json:"user,omitempty"`
	Port    int    `json:"port,omitempty"`
	URL     string `json:"url,omitempty"`
	Favicon string `json:"favicon,omitempty"`
}

type HostConfig struct {
	User               string `json:"user"`
	Port               int    `json:"port"`
	KeyName            string `json:"keyName"` // credential name in Windows Credential Manager
	Nickname           string `json:"nickname"`
	URL                string `json:"url"`                // preferred browser URL for this tailnet host
	HostKey            string `json:"hostKey"`            // authorized_keys-format SSH host key (TOFU)
	HostKeyFingerprint string `json:"hostKeyFingerprint"` // SHA256 fingerprint for display/audit
}

func defaultConfig() *AppConfig {
	return &AppConfig{
		UnlockMethod: "dpapi",
		WindowWidth:  1200,
		WindowHeight: 800,
		Hosts:        make(map[string]HostConfig),
	}
}

func configDir() string {
	dir, err := configDirPath()
	if err != nil {
		return filepath.Join(".", "womprat")
	}
	_ = os.MkdirAll(dir, 0700)
	return dir
}

func configDirPath() (string, error) {
	appData, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(appData) == "" {
		return "", fmt.Errorf("user config dir unavailable: %w", err)
	}
	return filepath.Join(appData, "womprat"), nil
}

func ensureConfigDir() (string, error) {
	dir, err := configDirPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func configPath() string {
	return filepath.Join(configDir(), "config.enc")
}

func configPathForWrite() (string, error) {
	dir, err := ensureConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.enc"), nil
}

// LoadConfig reads and decrypts the config file
// On Windows: DPAPI (only same user can decrypt)
// On other: AES-GCM with machine-local key
func LoadConfig() (*AppConfig, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}

	decrypted, err := decryptConfig(data)
	if err != nil {
		// If decryption fails (corrupt, wrong user, etc.), start fresh
		return defaultConfig(), nil
	}

	var cfg AppConfig
	if err := json.Unmarshal(decrypted, &cfg); err != nil {
		return defaultConfig(), nil
	}
	normalizeConfig(&cfg)
	return &cfg, nil
}

func normalizeConfig(cfg *AppConfig) {
	if cfg.Hosts == nil {
		cfg.Hosts = make(map[string]HostConfig)
	}
	if cfg.UnlockMethod == "hello" || cfg.UnlockMethod == "" || (cfg.UnlockMethod != "master" && cfg.UnlockMethod != "dpapi") {
		cfg.UnlockMethod = "dpapi"
	}
	if cfg.WindowWidth <= 0 {
		cfg.WindowWidth = 1200
	}
	if cfg.WindowHeight <= 0 {
		cfg.WindowHeight = 800
	}
	if cfg.FontSize < 0 || cfg.FontSize > 72 {
		cfg.FontSize = 0
	}
}

// SaveConfig encrypts and writes the config
func SaveConfig(cfg *AppConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	normalizeConfig(cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	encrypted, err := encryptConfig(data)
	if err != nil {
		return err
	}

	path, err := configPathForWrite()
	if err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0600)
}

// Credential store operations (Windows Credential Manager)
const (
	credPrefix = "womprat/"
)

// SaveCredential stores an encrypted secret
func SaveCredential(name, value string) error {
	if err := validateCredentialName(name); err != nil {
		return err
	}
	encrypted, err := encryptConfig([]byte(value))
	if err != nil {
		return err
	}
	dir, err := ensureConfigDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "creds", name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, encrypted, 0600)
}

// GetCredential retrieves and decrypts a stored secret
func GetCredential(name string) (string, error) {
	if err := validateCredentialName(name); err != nil {
		return "", err
	}
	path := filepath.Join(configDir(), "creds", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	decrypted, err := decryptConfig(data)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

// DeleteCredential removes a secret
func DeleteCredential(name string) error {
	if err := validateCredentialName(name); err != nil {
		return err
	}
	path := filepath.Join(configDir(), "creds", name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var credentialNamePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func validateCredentialName(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || filepath.Clean(name) != name || !credentialNamePattern.MatchString(name) {
		return fmt.Errorf("invalid credential name")
	}
	return nil
}

func safeSSHKeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty SSH key name")
	}
	if strings.ContainsAny(name, `/\\`) || strings.Contains(name, "..") || !credentialNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid SSH key name")
	}
	return name, nil
}
