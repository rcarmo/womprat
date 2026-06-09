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
