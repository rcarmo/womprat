package main

// config.go — self-contained settings management
// All settings stored in %APPDATA%/womprat/config.enc (DPAPI-encrypted JSON)
// Secrets in Windows Credential Manager

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// AppConfig holds all non-secret settings (DPAPI-encrypted on disk)
type AppConfig struct {
	// Unlock method: "hello" (Windows Hello), "master" (password), "dpapi" (transparent)
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

	// Appearance
	FontSize    int    `json:"fontSize"`
	Theme       string `json:"theme"`
	RestoreTabs bool   `json:"restoreTabs"`
	AutoConnect bool   `json:"autoConnect"`
}

type SavedTab struct {
	Type  string `json:"type"`  // "terminal" or "browser"
	Title string `json:"title"`
	Host  string `json:"host,omitempty"`
	User  string `json:"user,omitempty"`
	URL   string `json:"url,omitempty"`
}

type HostConfig struct {
	User       string `json:"user"`
	Port       int    `json:"port"`
	KeyName    string `json:"keyName"` // credential name in Windows Credential Manager
	Nickname   string `json:"nickname"`
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
	appData, _ := os.UserConfigDir()
	dir := filepath.Join(appData, "womprat")
	os.MkdirAll(dir, 0700)
	return dir
}

func configPath() string {
	return filepath.Join(configDir(), "config.enc")
}

// LoadConfig reads and decrypts the config file
// On Windows this uses DPAPI; on other platforms it's plaintext (dev mode)
func LoadConfig() (*AppConfig, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, err
	}

	// TODO: DPAPI decrypt on Windows
	// decrypted, err := dpapi.DecryptBytes(data)
	// For now, treat as plaintext JSON
	decrypted := data

	var cfg AppConfig
	if err := json.Unmarshal(decrypted, &cfg); err != nil {
		return defaultConfig(), nil
	}
	if cfg.Hosts == nil {
		cfg.Hosts = make(map[string]HostConfig)
	}
	return &cfg, nil
}

// SaveConfig encrypts and writes the config
func SaveConfig(cfg *AppConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// TODO: DPAPI encrypt on Windows
	// encrypted, err := dpapi.EncryptBytes(data)
	encrypted := data

	return os.WriteFile(configPath(), encrypted, 0600)
}

// Credential store operations (Windows Credential Manager)
const (
	credPrefix = "womprat/"
)

// SaveCredential stores a secret in Windows Credential Manager
func SaveCredential(name, value string) error {
	// TODO: Use github.com/danieljoos/wincred on Windows
	// Fallback for dev: write to config dir
	path := filepath.Join(configDir(), "creds", name)
	os.MkdirAll(filepath.Dir(path), 0700)
	return os.WriteFile(path, []byte(value), 0600)
}

// GetCredential retrieves a secret from Windows Credential Manager
func GetCredential(name string) (string, error) {
	// TODO: Use github.com/danieljoos/wincred on Windows
	path := filepath.Join(configDir(), "creds", name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeleteCredential removes a secret
func DeleteCredential(name string) error {
	path := filepath.Join(configDir(), "creds", name)
	return os.Remove(path)
}
