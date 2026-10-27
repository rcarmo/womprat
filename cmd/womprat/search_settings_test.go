package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchProviderPersistsAndResolveUsesSavedChoice(t *testing.T) {
	app := newTestApp(t)
	for _, provider := range []string{"bing", "google", "duckduckgo"} {
		w := performJSON(app.handleSearchProvider, "POST", "/api/settings/browser/search", map[string]string{"searchProvider": provider})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		cfg, err := LoadConfig()
		if err != nil || cfg.SearchProvider != provider {
			t.Fatal("provider did not persist", cfg, err)
		}
		resolved := performJSON(app.handleResolveAddress, "POST", "/api/settings/browser/resolve-address", map[string]string{"input": "test & query"})
		var out map[string]string
		json.Unmarshal(resolved.Body.Bytes(), &out)
		want, _ := resolveAddressInput("test & query", provider)
		if resolved.Code != 200 || out["url"] != want {
			t.Fatal("saved choice not used", out)
		}
	}
}
func TestSearchProviderInvalidOrFailedSaveDoesNotChangeMemory(t *testing.T) {
	app := newTestApp(t)
	app.config.SearchProvider = "bing"
	w := performJSON(app.handleSearchProvider, "POST", "/api/settings/browser/search", map[string]string{"searchProvider": "https://evil.example/"})
	if w.Code != 400 || app.config.SearchProvider != "bing" {
		t.Fatal("invalid provider saved")
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	w = performJSON(app.handleSearchProvider, "POST", "/api/settings/browser/search", map[string]string{"searchProvider": "google"})
	if w.Code != 500 || app.config.SearchProvider != "bing" {
		t.Fatal("failed save changed memory")
	}
}
func TestSearchRoutesRequireAuthAndRejectUnsafeAddress(t *testing.T) {
	app := newTestApp(t)
	mux := http.NewServeMux()
	app.registerBrowserRoutes(mux)
	for _, path := range []string{"/api/settings/browser/search", "/api/settings/browser/resolve-address", "/api/settings/browser/history"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"input":"hello","searchProvider":"google"}`))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("route lacks authentication", path, w.Code)
		}
	}
	w := performJSON(app.handleResolveAddress, "POST", "/api/settings/browser/resolve-address", map[string]string{"input": "javascript:alert(1)"})
	if w.Code != 400 {
		t.Fatal("unsafe scheme searched")
	}
	w = performJSON(app.handleResolveAddress, "GET", "/api/settings/browser/resolve-address", nil)
	if w.Code != 405 {
		t.Fatal("search text could be passed in URL")
	}
}
