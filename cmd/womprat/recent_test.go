package main

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRemoveRecentPersistsWithoutClosingOrChangingOpenTabs(t *testing.T) {
	a := newTestApp(t)
	a.config.OpenTabs = []SavedTab{{Type: "browser", URL: "https://example.com/", Title: "Example"}}
	a.tabs = []Tab{{ID: "browser-test", Type: "browser", URL: "https://example.com/"}}
	before := append([]SavedTab(nil), a.config.OpenTabs...)
	for i := 0; i < 2; i++ {
		w := performJSON(a.handleRemoveRecent, http.MethodPost, "/api/settings/recent/remove", map[string]string{"key": "browser:https://example.com/"})
		if w.Code != http.StatusOK {
			t.Fatalf("remove: %d %s", w.Code, w.Body.String())
		}
	}
	if !reflect.DeepEqual(a.config.OpenTabs, before) || len(a.tabs) != 1 || len(a.config.HiddenRecent) != 1 {
		t.Fatalf("remove changed tabs: %+v %+v", a.config, a.tabs)
	}
	// Both frontend and native tab snapshot writes must retain the dismissal.
	w := performJSON(a.handleSaveTabs, http.MethodPost, "/api/settings/save-tabs", map[string]any{"tabs": before})
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d", w.Code)
	}
	a.persistOpenTabs()
	cfg, err := LoadConfig()
	if err != nil || len(cfg.HiddenRecent) != 1 || cfg.HiddenRecent[0] != "browser:https://example.com/" {
		t.Fatalf("reload: %+v %v", cfg, err)
	}
	clone := cloneConfig(cfg)
	clone.HiddenRecent[0] = "changed"
	if cfg.HiddenRecent[0] == "changed" {
		t.Fatal("config clone aliases dismissed keys")
	}
}

func TestRemoveRecentSaveFailureAndInvalidInput(t *testing.T) {
	a := newTestApp(t)
	for _, key := range []string{"", "a\nb"} {
		w := performJSON(a.handleRemoveRecent, http.MethodPost, "/api/settings/recent/remove", map[string]string{"key": key})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("key %q: %d", key, w.Code)
		}
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	w := performJSON(a.handleRemoveRecent, http.MethodPost, "/api/settings/recent/remove", map[string]string{"key": "browser:https://example.com/"})
	if w.Code != http.StatusInternalServerError || len(a.config.HiddenRecent) != 0 {
		t.Fatalf("failed save mutated memory: %d %+v", w.Code, a.config.HiddenRecent)
	}
}
