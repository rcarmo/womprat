package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChromeOverlayJSContainsBindings(t *testing.T) {
	js := chromeOverlayJS(1234, "tok")
	for _, want := range []string{"womprat-chrome", "womprat-url", "womprat_getTabs", "tok", "1234"} {
		if !strings.Contains(js, want) {
			t.Fatalf("chromeOverlayJS missing %q", want)
		}
	}
}

func TestHandleSSHResize(t *testing.T) {
	app := newTestApp(t)
	rr := httptest.NewRecorder()
	app.handleSSHResize(rr, httptest.NewRequest("POST", "/api/ssh/resize", nil))
	if rr.Code != 200 {
		t.Fatalf("resize status = %d", rr.Code)
	}
}

func TestSetTailscaleKeyValidation(t *testing.T) {
	app := newTestApp(t)
	rr := performJSON(app.handleSetTailscaleKey, "GET", "/api/settings/tailscale-key", nil)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET set key = %d", rr.Code)
	}
	rr = performJSON(app.handleSetTailscaleKey, "POST", "/api/settings/tailscale-key", map[string]string{"key": "  "})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty set key = %d", rr.Code)
	}
}

func TestImportSSHKeyInvalid(t *testing.T) {
	app := newTestApp(t)
	if err := app.importSSHKey("bad/name", "not a key"); err == nil {
		t.Fatal("expected invalid key name")
	}
	if err := app.importSSHKey("valid", "not a key"); err == nil {
		t.Fatal("expected invalid key content")
	}
}

func TestRegisterSettingsAndBrowserRoutes(t *testing.T) {
	app := newTestApp(t)
	mux := http.NewServeMux()
	app.registerSettingsRoutes(mux)
	app.registerBrowserRoutes(mux)
	for _, path := range []string{"/api/settings/config", "/api/settings/browser"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s without token = %d", path, rr.Code)
		}
	}
}
