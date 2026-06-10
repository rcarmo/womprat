package main

// config.go — self-contained settings management
// All settings stored in %APPDATA%/womprat/config.enc (DPAPI-encrypted JSON)
// Secrets in Windows Credential Manager

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
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
		log.Printf("config dir lookup failed: %v", err)
		return filepath.Join(".", "womprat")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("config dir create failed for %s: %v", dir, err)
	}
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

func cloneConfig(cfg *AppConfig) *AppConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if cfg.OpenTabs != nil {
		out.OpenTabs = append([]SavedTab(nil), cfg.OpenTabs...)
	}
	if cfg.Hosts != nil {
		out.Hosts = make(map[string]HostConfig, len(cfg.Hosts))
		for host, conf := range cfg.Hosts {
			out.Hosts[host] = conf
		}
	}
	return &out
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
	cfg.FontSize = normalizeFontSize(cfg.FontSize)
	cfg.Theme = normalizeTheme(cfg.Theme)
	cfg.Hosts = sanitizeHostConfigs(cfg.Hosts)
	cfg.OpenTabs = sanitizeSavedTabs(cfg.OpenTabs)
}

func normalizeFontSize(size int) int {
	if size < 10 || size > 32 {
		return 0
	}
	return size
}

func normalizeTheme(_ string) string {
	return "dark"
}

func sanitizeHostConfigs(hosts map[string]HostConfig) map[string]HostConfig {
	out := make(map[string]HostConfig, len(hosts))
	for host, conf := range hosts {
		host = strings.TrimSpace(host)
		if validateCustomURLHost("ssh", host) != nil {
			continue
		}
		conf.User = strings.TrimSpace(conf.User)
		if conf.User != "" && validateCustomURLUser("ssh", conf.User) != nil {
			conf.User = ""
		}
		if conf.Port < 0 || conf.Port > 65535 {
			conf.Port = 0
		}
		conf.KeyName = strings.TrimSpace(conf.KeyName)
		if conf.KeyName != "" {
			if _, err := safeSSHKeyName(conf.KeyName); err != nil {
				conf.KeyName = ""
			}
		}
		conf.Nickname = strings.TrimSpace(conf.Nickname)
		if len(conf.Nickname) > 100 {
			conf.Nickname = conf.Nickname[:100]
		}
		conf.URL = sanitizeHostConfigURL(conf.URL)
		out[host] = conf
	}
	return out
}

func sanitizeHostConfigURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return parsed.String()
}

func sanitizeSavedTabs(tabs []SavedTab) []SavedTab {
	out := make([]SavedTab, 0, len(tabs))
	for _, tab := range tabs {
		sanitized, ok := sanitizeSavedTab(tab)
		if ok {
			out = append(out, sanitized)
		}
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

func sanitizeSavedTab(tab SavedTab) (SavedTab, bool) {
	tab.Type = strings.ToLower(strings.TrimSpace(tab.Type))
	tab.Title = sanitizeBrowserTitle(tab.Title)
	tab.Host = strings.TrimSpace(tab.Host)
	tab.User = strings.TrimSpace(tab.User)
	tab.URL = strings.TrimSpace(tab.URL)
	tab.Favicon = sanitizeFaviconURL(tab.Favicon)
	switch tab.Type {
	case "browser":
		parsed, err := url.Parse(tab.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return SavedTab{}, false
		}
		if tab.Title == "" {
			tab.Title = tab.URL
		}
		return tab, true
	case "terminal":
		if err := validateCustomURLHost("ssh", tab.Host); err != nil {
			return SavedTab{}, false
		}
		if tab.User == "" {
			tab.User = "root"
		}
		if err := validateCustomURLUser("ssh", tab.User); err != nil {
			return SavedTab{}, false
		}
		if tab.Port == 0 {
			tab.Port = 22
		}
		if tab.Port <= 0 || tab.Port > 65535 {
			return SavedTab{}, false
		}
		if tab.Title == "" {
			tab.Title = tab.Host
		}
		return tab, true
	case "vnc", "rdp":
		customURL := tab.URL
		if customURL == "" && tab.Host != "" {
			custom := customURLTarget{Scheme: tab.Type, Host: tab.Host, Port: tab.Port, User: tab.User}
			if custom.Port == 0 {
				if p, ok := customSchemeDefaultPort(tab.Type); ok {
					custom.Port = p
				}
			}
			customURL = custom.canonicalURL()
		}
		custom, err := parseCustomURL(customURL)
		if err != nil || custom.Scheme != tab.Type {
			return SavedTab{}, false
		}
		tab.Host = custom.Host
		tab.Port = custom.Port
		tab.User = custom.User
		tab.URL = custom.canonicalURL()
		if tab.Title == "" {
			tab.Title = tab.URL
		}
		return tab, true
	default:
		return SavedTab{}, false
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
