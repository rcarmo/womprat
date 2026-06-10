package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func performJSON(handler http.HandlerFunc, method, path string, body any) *httptest.ResponseRecorder {
	var rbody *bytes.Reader
	if body == nil {
		rbody = bytes.NewReader(nil)
	} else {
		data, _ := json.Marshal(body)
		rbody = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, rbody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestAuthAndSSHHandlersRejectMalformedJSON(t *testing.T) {
	app := newTestApp(t)
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
	}{
		{name: "unlock", handler: app.handleUnlock, path: "/api/auth/unlock"},
		{name: "save key", handler: app.handleSaveKey, path: "/api/key"},
		{name: "ssh connect", handler: app.handleSSHConnect, path: "/api/ssh/connect"},
		{name: "ssh password", handler: app.handleSSHAuthPassword, path: "/api/ssh/auth-password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(`{"x":true} trailing`))
			rr := httptest.NewRecorder()
			tc.handler(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestSSHAuthPasswordValidatesTabID(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSSHAuthPassword, "POST", "/api/ssh/auth-password", map[string]string{"tabId": "bad/id", "password": "x"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid password-auth tab id = %d %s", rr.Code, rr.Body.String())
	}
}

func TestSSHConnectValidatesBoundaryInputs(t *testing.T) {
	app := newTestApp(t)
	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{name: "bad host", body: map[string]any{"host": "bad host", "user": "root", "port": 22}},
		{name: "bad user", body: map[string]any{"host": "platinum", "user": "bad user", "port": 22}},
		{name: "bad port", body: map[string]any{"host": "platinum", "user": "root", "port": 70000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := performJSON(app.handleSSHConnect, "POST", "/api/ssh/connect", tc.body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestAuthStatusAndUnlock(t *testing.T) {
	app := newTestApp(t)
	app.config.UnlockMethod = "master"
	app.locked = true

	rr := performJSON(app.handleAuthStatus, "GET", "/api/auth/status", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"locked":true`) {
		t.Fatalf("auth status = %d %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(app.handleUnlock, "GET", "/api/auth/unlock", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET unlock = %d", rr.Code)
	}
	rr = performJSON(app.handleUnlock, "POST", "/api/auth/unlock", map[string]string{"password": "bad"})
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad unlock = %d %s", rr.Code, rr.Body.String())
	}
}

func TestSettingsUnlockAndMasterPasswordHandlers(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSetUnlockMethod, "GET", "/api/settings/unlock-method", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET unlock method = %d", rr.Code)
	}
	rr = performJSON(app.handleSetUnlockMethod, "POST", "/api/settings/unlock-method", map[string]string{"method": "hello"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad method = %d", rr.Code)
	}
	rr = performJSON(app.handleSetUnlockMethod, "POST", "/api/settings/unlock-method", map[string]string{"method": "master"})
	if rr.Code != 200 || app.config.UnlockMethod != "master" {
		t.Fatalf("set method = %d %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(app.handleSetMasterPassword, "POST", "/api/settings/master-password", map[string]string{"password": strings.Repeat("x", 4097)})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("oversized master password = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleSetMasterPassword, "POST", "/api/settings/master-password", map[string]string{"password": "secret"})
	if rr.Code != 200 {
		t.Fatalf("set master password = %d %s", rr.Code, rr.Body.String())
	}
	ok, err := verifyMasterPassword("secret")
	if err != nil || !ok {
		t.Fatalf("verify master = %v %v", ok, err)
	}
}

func TestHostAndAppearanceHandlersDoNotChangeMemoryOnPersistFailure(t *testing.T) {
	app := newTestApp(t)
	app.config.Hosts["smith"] = HostConfig{User: "old", Port: 22}
	app.config.FontSize = 14
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)

	rr := performJSON(app.handleHosts, "PATCH", "/api/settings/hosts/smith", map[string]any{"user": "new", "port": 2222})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("host persist failure = %d %s", rr.Code, rr.Body.String())
	}
	if got := app.config.Hosts["smith"]; got.User != "old" || got.Port != 22 {
		t.Fatalf("host changed despite persist failure: %+v", got)
	}

	rr = performJSON(app.handleAppearance, "POST", "/api/settings/appearance", map[string]any{"fontSize": 16, "theme": "dark", "restoreTabs": true, "autoConnect": true})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("appearance persist failure = %d %s", rr.Code, rr.Body.String())
	}
	if app.config.FontSize != 14 || app.config.RestoreTabs || app.config.AutoConnect {
		t.Fatalf("appearance changed despite persist failure: %+v", app.config)
	}
}

func TestHostsAppearanceAndSaveTabsHandlers(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleHosts, "PATCH", "/api/settings/hosts/smith", map[string]any{"user": "rui", "port": 2222, "nickname": "Smith", "url": "http://smith"})
	if rr.Code != 200 {
		t.Fatalf("patch host = %d %s", rr.Code, rr.Body.String())
	}
	if got := app.config.Hosts["smith"].Port; got != 2222 {
		t.Fatalf("host port = %d", got)
	}
	rr = performJSON(app.handleHosts, "PATCH", "/api/settings/hosts/smith", map[string]any{"port": 70000})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad host port = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleHosts, "PATCH", "/api/settings/hosts/smith", map[string]any{"user": "bad user"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad host user = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleHosts, "PATCH", "/api/settings/hosts/bad/extra", map[string]any{"port": 22})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad host path = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleHosts, "GET", "/api/settings/hosts", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "smith") {
		t.Fatalf("get hosts = %d %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(app.handleAppearance, "POST", "/api/settings/appearance", map[string]any{"fontSize": 16, "theme": "light", "restoreTabs": true, "autoConnect": true})
	if rr.Code != 200 || app.config.FontSize != 16 || app.config.Theme != "dark" || !app.config.RestoreTabs || !app.config.AutoConnect {
		t.Fatalf("appearance = %d %+v", rr.Code, app.config)
	}
	rr = performJSON(app.handleAppearance, "POST", "/api/settings/appearance", map[string]any{"fontSize": 99, "theme": "light"})
	if rr.Code != 200 || app.config.FontSize != 0 || app.config.Theme != "dark" {
		t.Fatalf("appearance bounds = %d %+v", rr.Code, app.config)
	}

	tabs := []SavedTab{{Type: "browser", Title: "Example", URL: "http://example.com"}}
	rr = performJSON(app.handleSaveTabs, "POST", "/api/settings/save-tabs", map[string]any{"tabs": tabs})
	if rr.Code != 200 || len(app.config.OpenTabs) != 1 {
		t.Fatalf("save tabs = %d %+v", rr.Code, app.config.OpenTabs)
	}
}

func TestSettingsRejectMalformedJSON(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest("POST", "/api/settings/unlock-method", strings.NewReader(`{"method":"master"} trailing`))
	rr := httptest.NewRecorder()
	app.handleSetUnlockMethod(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed json = %d %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(app.handleSetUnlockMethod, "POST", "/api/settings/unlock-method", map[string]any{"method": "master", "extra": true})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown field json = %d %s", rr.Code, rr.Body.String())
	}
}

func TestSSHKeyHandlers(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleGenerateSSHKey, "POST", "/api/settings/ssh-keys/generate", map[string]string{"name": "main"})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "publicKey") {
		t.Fatalf("generate key = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleSSHKeys, "GET", "/api/settings/ssh-keys", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "main") {
		t.Fatalf("list keys = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleSSHKeys, "DELETE", "/api/settings/ssh-keys/main", nil)
	if rr.Code != 200 {
		t.Fatalf("delete key = %d %s", rr.Code, rr.Body.String())
	}
}

func TestDebugLogRejectsMalformedJSON(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest("POST", "/api/settings/debug-log", strings.NewReader(`{"enabled":true} trailing`))
	rr := httptest.NewRecorder()
	app.handleDebugLog(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed debug-log json = %d %s", rr.Code, rr.Body.String())
	}
}

func TestBrowserCacheSizeHelpers(t *testing.T) {
	if got := formatByteSize(-1); got != "0 B" {
		t.Fatalf("negative format = %q", got)
	}
	if got := formatByteSize(1536); got != "1.5 KB" {
		t.Fatalf("kb format = %q", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if size, err := cacheSizeBytes(dir); err != nil || size != 3 {
		t.Fatalf("cacheSizeBytes = %d %v", size, err)
	}
	if size, err := cacheSizeBytes(filepath.Join(dir, "missing")); err != nil || size != 0 {
		t.Fatalf("missing cacheSizeBytes = %d %v", size, err)
	}
}

func TestBrowserCleanupHelpersIgnoreMissingPaths(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := removeExistingFiles(missing); err != nil {
		t.Fatalf("removeExistingFiles missing = %v", err)
	}
	if err := removeExistingPaths(missing); err != nil {
		t.Fatalf("removeExistingPaths missing = %v", err)
	}
}

func TestSavePasswordsToggleDoesNotChangeMemoryOnPersistFailure(t *testing.T) {
	app := newTestApp(t)
	app.config.SavePasswords = false
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)
	rr := performJSON(app.handleSavePasswordsToggle, "POST", "/api/settings/browser/save-passwords", map[string]bool{"enabled": true})
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("save passwords persist failure = %d %s", rr.Code, rr.Body.String())
	}
	if app.config.SavePasswords {
		t.Fatal("save passwords changed in memory despite persist failure")
	}
}

func TestBrowserSettingsHandlers(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleBrowserData, "GET", "/api/settings/browser", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "cacheSize") {
		t.Fatalf("browser data = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleSavePasswordsToggle, "POST", "/api/settings/browser/save-passwords", map[string]bool{"enabled": true})
	if rr.Code != 200 || !app.config.SavePasswords {
		t.Fatalf("save passwords toggle = %d %+v", rr.Code, app.config)
	}
	rr = performJSON(app.handleClearCookies, "POST", "/api/settings/browser/clear-cookies", map[string]string{"domain": "bad host"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad cookie domain = %d %s", rr.Code, rr.Body.String())
	}
	rr = performJSON(app.handleClearPasswords, "POST", "/api/settings/browser/clear-passwords", map[string]string{"site": "../secret"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad password site = %d %s", rr.Code, rr.Body.String())
	}
	req := httptest.NewRequest("POST", "/api/settings/browser/save-passwords", strings.NewReader(`{"enabled":true} trailing`))
	rr = httptest.NewRecorder()
	app.handleSavePasswordsToggle(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed save-passwords json = %d %s", rr.Code, rr.Body.String())
	}
	for _, h := range []http.HandlerFunc{app.handleClearCache, app.handleClearCookies, app.handleClearPasswords, app.handleClearAll} {
		rr = performJSON(h, "POST", "/", nil)
		if rr.Code != 200 {
			t.Fatalf("clear handler = %d %s", rr.Code, rr.Body.String())
		}
	}
}
