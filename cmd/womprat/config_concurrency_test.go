package main

import (
	"net/http"
	"sync"
	"testing"
)

func TestConcurrentSettingsPreserveIndependentFields(t *testing.T) {
	app := newTestApp(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, run := range []func(){
		func() {
			r := performJSON(app.handleAppearance, http.MethodPost, "/api/settings/appearance", map[string]any{"fontSize": 18, "theme": "dark", "restoreTabs": true})
			if r.Code != 200 {
				t.Errorf("appearance: %d %s", r.Code, r.Body.String())
			}
		},
		func() {
			r := performJSON(app.handleSavePasswordsToggle, http.MethodPost, "/api/settings/browser/save-passwords", map[string]bool{"enabled": true})
			if r.Code != 200 {
				t.Errorf("passwords: %d %s", r.Code, r.Body.String())
			}
		},
	} {
		wg.Add(1)
		go func(f func()) { defer wg.Done(); <-start; f() }(run)
	}
	close(start)
	wg.Wait()
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FontSize != 18 || !cfg.RestoreTabs || !cfg.SavePasswords {
		t.Fatalf("independent settings lost: %+v", cfg)
	}
}
