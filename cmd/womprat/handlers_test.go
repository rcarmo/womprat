package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	rr = performJSON(app.handleSetMasterPassword, "POST", "/api/settings/master-password", map[string]string{"password": "secret"})
	if rr.Code != 200 {
		t.Fatalf("set master password = %d %s", rr.Code, rr.Body.String())
	}
	ok, err := verifyMasterPassword("secret")
	if err != nil || !ok {
		t.Fatalf("verify master = %v %v", ok, err)
	}
}

func TestHostsAppearanceAndSaveTabsHandlers(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleHosts, "PATCH", "/api/settings/hosts/smith", map[string]any{"user": "rui", "port": 2222.0, "nickname": "Smith", "url": "http://smith"})
	if rr.Code != 200 {
		t.Fatalf("patch host = %d %s", rr.Code, rr.Body.String())
	}
	if got := app.config.Hosts["smith"].Port; got != 2222 {
		t.Fatalf("host port = %d", got)
	}
	rr = performJSON(app.handleHosts, "GET", "/api/settings/hosts", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "smith") {
		t.Fatalf("get hosts = %d %s", rr.Code, rr.Body.String())
	}

	rr = performJSON(app.handleAppearance, "POST", "/api/settings/appearance", map[string]any{"fontSize": 16, "theme": "dark", "restoreTabs": true, "autoConnect": true})
	if rr.Code != 200 || app.config.FontSize != 16 || !app.config.RestoreTabs || !app.config.AutoConnect {
		t.Fatalf("appearance = %d %+v", rr.Code, app.config)
	}

	tabs := []SavedTab{{Type: "browser", Title: "Example", URL: "http://example.com"}}
	rr = performJSON(app.handleSaveTabs, "POST", "/api/settings/save-tabs", map[string]any{"tabs": tabs})
	if rr.Code != 200 || len(app.config.OpenTabs) != 1 {
		t.Fatalf("save tabs = %d %+v", rr.Code, app.config.OpenTabs)
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
	for _, h := range []http.HandlerFunc{app.handleClearCache, app.handleClearCookies, app.handleClearPasswords, app.handleClearAll} {
		rr = performJSON(h, "POST", "/", nil)
		if rr.Code != 200 {
			t.Fatalf("clear handler = %d %s", rr.Code, rr.Body.String())
		}
	}
}
