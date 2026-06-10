package main

import "testing"

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
	if cfg.UnlockMethod != "dpapi" || cfg.WindowWidth != 1200 || cfg.WindowHeight != 800 || cfg.FontSize != 0 || cfg.Hosts == nil {
		t.Fatalf("normalized config = %+v", cfg)
	}
}

func TestSanitizeSavedTabs(t *testing.T) {
	tabs := sanitizeSavedTabs([]SavedTab{
		{Type: "browser", URL: "https://example.com"},
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
	if tabs[0].Type != "browser" || tabs[0].Title != "https://example.com" {
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
