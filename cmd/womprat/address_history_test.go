package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAddressHistoryPersistsAndDeduplicates(t *testing.T) {
	app := newTestApp(t)
	for _, input := range []string{"weather tomorrow", "https://flint.local:8443/", "weather tomorrow"} {
		w := performJSON(app.handleAddressHistory, "POST", "/api/settings/browser/history", map[string]string{"input": input})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	cfg, err := LoadConfig()
	if err != nil || len(cfg.AddressHistory) != 2 || cfg.AddressHistory[0] != "weather tomorrow" {
		t.Fatal("history not persisted", cfg, err)
	}
	copy := cloneConfig(cfg)
	copy.AddressHistory[0] = "changed"
	if cfg.AddressHistory[0] == "changed" {
		t.Fatal("history aliases cloned config")
	}
	for i := 0; i < 110; i++ {
		cfg.AddressHistory = append(cfg.AddressHistory, "query "+string(rune('一'+i)))
	}
	normalizeConfig(cfg)
	if len(cfg.AddressHistory) != 100 {
		t.Fatal("history not bounded", len(cfg.AddressHistory))
	}
	w := httptest.NewRecorder()
	app.handleAddressHistory(w, httptest.NewRequest("GET", "/api/settings/browser/history", nil))
	var out struct {
		History []string `json:"history"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.History) != 2 {
		t.Fatal("history GET missing")
	}
}
func TestAddressHistoryValidatesAndFailsTransactionally(t *testing.T) {
	app := newTestApp(t)
	app.config.AddressHistory = []string{"previous search"}
	for _, input := range []string{"javascript:alert(1)", "https://user:password@example.com/", "file:///secret"} {
		w := performJSON(app.handleAddressHistory, "POST", "/api/settings/browser/history", map[string]string{"input": input})
		if w.Code != 400 {
			t.Fatal("unsafe history accepted")
		}
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	os.WriteFile(blocked, []byte("file"), 0600)
	t.Setenv("XDG_CONFIG_HOME", blocked)
	w := performJSON(app.handleAddressHistory, "POST", "/api/settings/browser/history", map[string]string{"input": "new search"})
	if w.Code != http.StatusInternalServerError || len(app.config.AddressHistory) != 1 || app.config.AddressHistory[0] != "previous search" {
		t.Fatal("failed save changed memory")
	}
}
