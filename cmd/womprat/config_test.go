package main

import (
	"strings"
	"testing"
)

func TestCredentialNameValidation(t *testing.T) {
	valid := []string{"tailscale-key", "ssh-key/main", "browser-pw/example.com", "a.b_c-1"}
	for _, name := range valid {
		if err := validateCredentialName(name); err != nil {
			t.Fatalf("%q should be valid: %v", name, err)
		}
	}
	invalid := []string{"", "/abs", "../x", "ssh-key/../x", "space name", "semi;colon", "back\\slash"}
	for _, name := range invalid {
		if err := validateCredentialName(name); err == nil {
			t.Fatalf("%q should be invalid", name)
		}
	}
}

func TestSafeSSHKeyName(t *testing.T) {
	if got, err := safeSSHKeyName(" main-key "); err != nil || got != "main-key" {
		t.Fatalf("safeSSHKeyName = %q %v", got, err)
	}
	for _, name := range []string{"", "../id", "dir/key", "bad key"} {
		if _, err := safeSSHKeyName(name); err == nil {
			t.Fatalf("%q should fail", name)
		}
	}
}

func TestSaveLoadDeleteCredential(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := SaveCredential("ssh-key/test", "secret"); err != nil {
		t.Fatal(err)
	}
	got, err := GetCredential("ssh-key/test")
	if err != nil || got != "secret" {
		t.Fatalf("GetCredential = %q %v", got, err)
	}
	if err := DeleteCredential("ssh-key/test"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetCredential("ssh-key/test"); err == nil {
		t.Fatal("expected missing credential after delete")
	}
}

func TestNormalizeConfigRepairsInvalidDefaults(t *testing.T) {
	cfg := &AppConfig{UnlockMethod: "bad", WindowWidth: -1, WindowHeight: 0, FontSize: 999}
	normalizeConfig(cfg)
	if cfg.UnlockMethod != "dpapi" || cfg.WindowWidth != 1200 || cfg.WindowHeight != 800 || cfg.FontSize != 0 || cfg.Theme != "dark" || cfg.Hosts == nil {
		t.Fatalf("normalized config = %+v", cfg)
	}
}

func TestNormalizeAppearance(t *testing.T) {
	for _, tt := range []struct{ in, want int }{{0, 0}, {9, 0}, {10, 10}, {16, 16}, {32, 32}, {33, 0}} {
		if got := normalizeFontSize(tt.in); got != tt.want {
			t.Fatalf("normalizeFontSize(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
	if got := normalizeTheme("light"); got != "dark" {
		t.Fatalf("normalizeTheme = %q", got)
	}
}

func TestSanitizeHostConfigs(t *testing.T) {
	hosts := sanitizeHostConfigs(map[string]HostConfig{
		"platinum": {User: " me ", Port: 22, KeyName: " main ", Nickname: strings.Repeat("n", 120), URL: "https://platinum.local"},
		"bad host": {User: "me", Port: 22},
		"baduser":  {User: "bad user", Port: 70000, KeyName: "../x", URL: "rdp://me@host"},
	})
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v", hosts)
	}
	if got := hosts["platinum"]; got.User != "me" || got.KeyName != "main" || len(got.Nickname) != 100 || got.URL != "https://platinum.local" {
		t.Fatalf("platinum host = %+v", got)
	}
	if got := hosts["baduser"]; got.User != "" || got.Port != 0 || got.KeyName != "" || got.URL != "" {
		t.Fatalf("baduser host = %+v", got)
	}
}

func TestSanitizeSavedTabs(t *testing.T) {
	tabs := sanitizeSavedTabs([]SavedTab{
		{Type: "browser", Title: strings.Repeat("x", maxBrowserTitleRunes+10), Favicon: strings.Repeat("x", maxFaviconURLBytes+1), URL: "https://example.com"},
		{Type: "browser", URL: "rdp://me@platinum:3389"},
		{Type: "terminal", Host: "platinum"},
		{Type: "vnc", Host: "platinum"},
		{Type: "rdp", URL: "rdp://me@platinum"},
		{Type: "rdp", URL: "rdp://bad host"},
		{Type: "unknown", URL: "https://example.com"},
	})
	if len(tabs) != 4 {
		t.Fatalf("sanitizeSavedTabs kept %d tabs: %+v", len(tabs), tabs)
	}
	if tabs[0].Type != "browser" || len([]rune(tabs[0].Title)) != maxBrowserTitleRunes || tabs[0].Favicon != "" {
		t.Fatalf("browser tab = %+v", tabs[0])
	}
	if tabs[1].Type != "terminal" || tabs[1].User != "root" || tabs[1].Port != 22 {
		t.Fatalf("terminal tab = %+v", tabs[1])
	}
	if tabs[2].Type != "vnc" || tabs[2].URL != "vnc://platinum:5900" {
		t.Fatalf("vnc tab = %+v", tabs[2])
	}
	if tabs[3].Type != "rdp" || tabs[3].URL != "rdp://me@platinum:3389" || tabs[3].User != "me" {
		t.Fatalf("rdp tab = %+v", tabs[3])
	}
}

func TestSaveConfigRejectsNil(t *testing.T) {
	if err := SaveConfig(nil); err == nil {
		t.Fatal("SaveConfig(nil) succeeded")
	}
}

func TestSaveLoadConfigDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := defaultConfig()
	cfg.Hosts["smith"] = HostConfig{User: "rui", Port: 22, URL: "http://smith"}
	cfg.OpenTabs = []SavedTab{{Type: "browser", Title: "Smith", URL: "http://smith"}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UnlockMethod != "dpapi" || loaded.Hosts["smith"].User != "rui" || len(loaded.OpenTabs) != 1 {
		t.Fatalf("loaded config mismatch: %+v", loaded)
	}
}
