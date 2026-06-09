package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

type fakeWebView struct{ urls []string }

func (f *fakeWebView) Navigate(url string) { f.urls = append(f.urls, url) }

func newTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return &App{
		config:       defaultConfig(),
		sshConns:     map[string]*ssh.Client{},
		pendingAuth:  map[string]*pendingSSH{},
		sessionToken: "test-token",
		webview:      &fakeWebView{},
	}
}

func TestGenerateSessionToken(t *testing.T) {
	a := generateSessionToken()
	b := generateSessionToken()
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("token length = %d/%d, want 64", len(a), len(b))
	}
	if a == b {
		t.Fatal("tokens should be random")
	}
}

func TestNavigateAndNewBrowserNormalizeURL(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "browser-1", Type: "browser", Title: "old", URL: "http://old"}}
	app.activeTab = "browser-1"

	app.navigateBrowser("example.com")
	if got := app.tabs[0].URL; got != "http://example.com" {
		t.Fatalf("navigate URL = %q", got)
	}
	if got := app.webview.(*fakeWebView).urls[0]; got != "http://example.com" {
		t.Fatalf("navigated webview to %q", got)
	}

	app.newBrowserTab("https://example.net")
	if got := app.tabs[len(app.tabs)-1].URL; got != "https://example.net" {
		t.Fatalf("new browser URL = %q", got)
	}
}

func TestUpdateActiveBrowserTitleIgnoresChromeErrorURL(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "b", Type: "browser", Title: "old", URL: "http://example.com", Favicon: "https://example.com/icon.png"}}
	app.activeTab = "b"
	app.updateActiveBrowserTitle("Error", "chrome-error://chromewebdata/", "chrome://favicon")
	if got := app.tabs[0].URL; got != "http://example.com" {
		t.Fatalf("URL overwritten by chrome error page: %q", got)
	}
	if got := app.tabs[0].Title; got != "Error" {
		t.Fatalf("title = %q", got)
	}
}

func TestCloseTabSelectsAdjacent(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "a", Type: "browser", URL: "http://a"}, {ID: "b", Type: "browser", URL: "http://b"}, {ID: "c", Type: "browser", URL: "http://c"}}
	app.activeTab = "b"
	app.closeTab("b")
	if got := app.activeTab; got != "c" {
		t.Fatalf("active after closing middle = %q, want c", got)
	}
	if got := app.webview.(*fakeWebView).urls[len(app.webview.(*fakeWebView).urls)-1]; got != "http://c" {
		t.Fatalf("navigated to %q", got)
	}
}

func TestReorderTab(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	app.reorderTab("c", 0)
	got := []string{app.tabs[0].ID, app.tabs[1].ID, app.tabs[2].ID}
	if strings.Join(got, ",") != "c,a,b" {
		t.Fatalf("order = %v", got)
	}
}

func TestAuthMiddleware(t *testing.T) {
	app := newTestApp(t)
	called := false
	h := app.authMiddleware(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })

	req := httptest.NewRequest("GET", "/x", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusForbidden || called {
		t.Fatalf("missing token status/called = %d/%v", rr.Code, called)
	}

	req = httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Session-Token", "test-token")
	rr = httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusNoContent || !called {
		t.Fatalf("valid token status/called = %d/%v", rr.Code, called)
	}
}

func TestReadSOCKSAddr(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	buf.WriteByte(11)
	buf.WriteString("example.com")
	_ = binary.Write(buf, binary.BigEndian, uint16(443))
	host, port, err := readSOCKSAddr(buf, 0x03)
	if err != nil || host != "example.com" || port != 443 {
		t.Fatalf("domain addr = %q %d %v", host, port, err)
	}

	ip := net.ParseIP("127.0.0.1").To4()
	buf.Reset()
	buf.Write(ip)
	_ = binary.Write(buf, binary.BigEndian, uint16(80))
	host, port, err = readSOCKSAddr(buf, 0x01)
	if err != nil || host != "127.0.0.1" || port != 80 {
		t.Fatalf("ipv4 addr = %q %d %v", host, port, err)
	}
}
