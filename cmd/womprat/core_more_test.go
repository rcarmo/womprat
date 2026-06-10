package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestRegisterRoutes(t *testing.T) {
	app := newTestApp(t)
	mux := http.NewServeMux()
	app.registerRoutes(mux)
	app.registerSettingsRoutes(mux)
	app.registerBrowserRoutes(mux)
	app.registerDownloadRoutes(mux)
	// ServeMux has no public route inspection; this verifies registration does not panic
	// and that an authenticated registered route is reachable.
	rr := performJSON(app.authMiddleware(app.handleAuthStatus), "GET", "/api/auth/status", nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("auth route without token = %d", rr.Code)
	}
}

func TestLocalTabStateHelpers(t *testing.T) {
	app := newTestApp(t)
	app.serverPort = 1234
	app.newTerminalTab("smith", "", 0)
	if len(app.tabs) != 1 || app.tabs[0].User != "root" || app.tabs[0].Port != 22 || app.activeTab != app.tabs[0].ID {
		t.Fatalf("terminal tab = %+v active=%q", app.tabs, app.activeTab)
	}
	if got := app.webview.(*fakeWebView).evals[0]; !strings.Contains(got, "activateTab") || !strings.Contains(got, "term-") {
		t.Fatalf("terminal shell eval = %q", got)
	}

	app.openSettingsTab()
	if app.activeTab != "settings" || app.tabs[len(app.tabs)-1].URL != "settings:" {
		t.Fatalf("settings tab = %+v active=%q", app.tabs, app.activeTab)
	}
	app.clearActiveTab()
	if app.activeTab != "" {
		t.Fatalf("clear active = %q", app.activeTab)
	}
	app.goHome()
	if got := app.webview.(*fakeWebView).urls[len(app.webview.(*fakeWebView).urls)-1]; !strings.Contains(got, "http://127.0.0.1:1234/") {
		t.Fatalf("home navigate = %q", got)
	}
}

func TestRegisterLocalTabAndUpsertPreservesFields(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "b", Type: "browser", Title: "Old", URL: "http://old", Favicon: "http://old/icon.png"}}
	app.registerLocalTab(`{"id":"b","type":"browser","title":"New"}`)
	if len(app.tabs) != 1 || app.tabs[0].URL != "http://old" || app.tabs[0].Favicon == "" || app.tabs[0].Title != "New" {
		t.Fatalf("upsert preserve = %+v", app.tabs)
	}
	app.registerLocalTab(`bad-json`)
	if len(app.tabs) != 1 {
		t.Fatalf("bad register changed tabs: %+v", app.tabs)
	}
	app.registerLocalTab(`{"id":"bad/id","type":"browser","url":"https://example.com"}`)
	app.registerLocalTab(`{"id":"x","type":"browser","url":"rdp://me@platinum:3389"}`)
	app.registerLocalTab(`{"id":"x","type":"unknown","url":"https://example.com"}`)
	if len(app.tabs) != 1 {
		t.Fatalf("invalid local tabs changed tabs: %+v", app.tabs)
	}
}

func TestHandleSaveKeyValidation(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSaveKey, "GET", "/api/auth/save-key", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET save key = %d", rr.Code)
	}
	rr = performJSON(app.handleSaveKey, "POST", "/api/auth/save-key", map[string]string{"key": ""})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty save key = %d", rr.Code)
	}
	rr = performJSON(app.handleSaveKey, "POST", "/api/auth/save-key", map[string]string{"key": "abc\nxyz"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid save key = %d", rr.Code)
	}
}

func TestSSHDisconnectedHandlers(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSSHConnect, "GET", "/api/ssh/connect", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET ssh connect = %d", rr.Code)
	}
	rr = performJSON(app.handleSSHConnect, "POST", "/api/ssh/connect", map[string]any{"host": ""})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty host = %d", rr.Code)
	}
	rr = performJSON(app.handleSSHConnect, "POST", "/api/ssh/connect", map[string]any{"host": "smith"})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("no tailscale = %d", rr.Code)
	}
	rr = performJSON(app.handleSSHAuthPassword, "GET", "/api/ssh/auth-password", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET ssh auth = %d", rr.Code)
	}
	rr = performJSON(app.handleSSHAuthPassword, "POST", "/api/ssh/auth-password", map[string]string{"tabId": "missing", "password": "x"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing pending auth = %d", rr.Code)
	}
}

func TestCryptoHelpers(t *testing.T) {
	key := deriveKey([]byte("seed"))
	if len(key) != 32 {
		t.Fatalf("key len = %d", len(key))
	}
	ciphertext, err := encryptAESGCM([]byte("hello"), key)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptAESGCM(ciphertext, key)
	if err != nil || string(plain) != "hello" {
		t.Fatalf("decrypt = %q %v", plain, err)
	}
	if _, err := decryptAESGCM([]byte("short"), key); err == nil {
		t.Fatal("expected short ciphertext error")
	}
	if _, err := encryptAESGCM([]byte("x"), []byte("bad")); err == nil {
		t.Fatal("expected bad key error")
	}
}
