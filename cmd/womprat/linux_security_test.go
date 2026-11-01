//go:build linux

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLinuxShellCSPDoesNotFetchRemoteResources(t *testing.T) {
	a := &App{config: defaultConfig(), serverPort: 32123, sessionToken: "test"}
	w := httptest.NewRecorder()
	a.serveFrontend(w, httptest.NewRequest("GET", "/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"connect-src 'self' ws://127.0.0.1:32123;", "frame-src 'self';", "img-src 'self' data: blob:;"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP missing %s: %s", want, csp)
		}
	}
}

func TestNativeLinuxPasswordSavingDoesNotPretendSupport(t *testing.T) {
	a := &App{config: defaultConfig(), clearLinuxBrowserData: func(int, string) error { return nil }}
	w := httptest.NewRecorder()
	a.handleSavePasswordsToggle(w, httptest.NewRequest("POST", "/api/settings/browser/save-passwords", strings.NewReader(`{"enabled":true}`)))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status %d", w.Code)
	}
}

func TestLinuxNativeSecurityContracts(t *testing.T) {
	source := readFileForRegression(t, "webkit_linux.c")
	for _, want := range []string{"webkit_web_context_set_sandbox_enabled(h->content, TRUE)", "webkit_web_context_set_sandbox_enabled(local, TRUE)", "webkit_settings_set_enable_webrtc", "webkit_network_proxy_settings_new(proxy, NULL)", "const char *current = h->shell_uri", "webkit_permission_request_deny(request)"} {
		if !strings.Contains(source, want) {
			t.Fatalf("native contract missing %q", want)
		}
	}
	if strings.Contains(source, "WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS") {
		t.Fatal("sandbox bypass present")
	}
}
