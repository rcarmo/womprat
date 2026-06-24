package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
)

type fakeWebView struct {
	urls  []string
	evals []string
}

func (f *fakeWebView) Navigate(url string) { f.urls = append(f.urls, url) }
func (f *fakeWebView) Eval(js string)      { f.evals = append(f.evals, js) }
func (f *fakeWebView) Resize()             {}

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

func TestNewTabIDIsValidAndUnique(t *testing.T) {
	a := newTabID("term")
	b := newTabID("term")
	if a == b || !validTabID(a) || !validTabID(b) {
		t.Fatalf("tab ids = %q %q", a, b)
	}
}

func TestJSStringFallsBackOnMarshalError(t *testing.T) {
	if got := jsString(func() {}); got != "null" {
		t.Fatalf("jsString unsupported = %q", got)
	}
	if got := jsString("x<y"); got != `"x\u003cy"` {
		t.Fatalf("jsString string = %q", got)
	}
}

func TestGenerateSessionToken(t *testing.T) {
	a, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
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
	if got := app.webview.(*fakeWebView).evals[0]; !strings.Contains(got, "showBrowserTab") || !strings.Contains(got, "http://example.com") {
		t.Fatalf("shell eval = %q", got)
	}

	app.newBrowserTab("https://example.net")
	if got := app.tabs[len(app.tabs)-1].URL; got != "https://example.net" {
		t.Fatalf("new browser URL = %q", got)
	}
}

func TestBrowserHotkeySanitizers(t *testing.T) {
	if !validBrowserHotkeyAction("focusUrl") || !validBrowserHotkeyAction("tabAt") {
		t.Fatal("expected browser hotkey actions rejected")
	}
	for _, action := range []string{"", "eval", "focusUrl;alert(1)", "unknown"} {
		if validBrowserHotkeyAction(action) {
			t.Fatalf("unexpected browser hotkey action accepted: %q", action)
		}
	}
	if got := sanitizeBrowserHotkeyArg(strings.Repeat("1", 32)); len(got) != 16 {
		t.Fatalf("hotkey arg length = %d", len(got))
	}
	if got := sanitizeBrowserHotkeyArg(strings.Repeat("界", 8)); len(got) > 16 || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("unicode hotkey arg = len %d %q", len(got), got)
	}
}

func TestBrowserMetadataSanitizers(t *testing.T) {
	if got := sanitizeBrowserTitle(strings.Repeat("x", maxBrowserTitleRunes+20)); len([]rune(got)) != maxBrowserTitleRunes {
		t.Fatalf("title length = %d", len([]rune(got)))
	}
	if got := sanitizeFaviconURL("javascript:alert(1)"); got != "" {
		t.Fatalf("javascript favicon accepted: %q", got)
	}
	if got := sanitizeFaviconURL("https://example.com/favicon.ico"); got == "" {
		t.Fatal("https favicon rejected")
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
	app.updateActiveBrowserTitle("Good", "https://example.com", "javascript:alert(1)")
	if got := app.tabs[0].Favicon; got != "https://example.com/icon.png" {
		t.Fatalf("unsafe favicon changed tab favicon: %q", got)
	}
}

func TestTabMutationsRejectInvalidIDs(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "a", Type: "browser", URL: "http://a"}, {ID: "b", Type: "browser", URL: "http://b"}}
	app.activeTab = "a"
	app.switchTab("bad/id")
	app.closeTab("bad/id")
	app.forgetTab("bad/id")
	app.reorderTab("bad/id", "a")
	if len(app.tabs) != 2 || app.activeTab != "a" {
		t.Fatalf("invalid tab mutation changed state: active=%q tabs=%+v", app.activeTab, app.tabs)
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
	if got := app.webview.(*fakeWebView).evals[len(app.webview.(*fakeWebView).evals)-1]; !strings.Contains(got, "activateTab") || !strings.Contains(got, "c") {
		t.Fatalf("shell eval = %q", got)
	}
}

func TestClampTabIndex(t *testing.T) {
	for _, tt := range []struct{ in, count, want int }{{-1, 3, 0}, {0, 3, 0}, {2, 3, 2}, {3, 3, 2}, {10, 3, 2}, {1, 0, 0}} {
		if got := clampTabIndex(tt.in, tt.count); got != tt.want {
			t.Fatalf("clampTabIndex(%d,%d) = %d, want %d", tt.in, tt.count, got, tt.want)
		}
	}
}

func TestReorderTab(t *testing.T) {
	app := newTestApp(t)
	app.tabs = []Tab{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	// Move c before a.
	app.reorderTab("c", "a")
	got := []string{app.tabs[0].ID, app.tabs[1].ID, app.tabs[2].ID}
	if strings.Join(got, ",") != "c,a,b" {
		t.Fatalf("order = %v", got)
	}
	// Empty/unknown beforeID appends to the end.
	app.reorderTab("c", "")
	got = []string{app.tabs[0].ID, app.tabs[1].ID, app.tabs[2].ID}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("append order = %v", got)
	}
	// Reorder against an array that differs from the shell's (extra settings tab)
	// must still place relative to the named id, not a precomputed index.
	app.tabs = []Tab{{ID: "settings"}, {ID: "a"}, {ID: "b"}, {ID: "c"}}
	app.reorderTab("c", "b")
	got = []string{app.tabs[0].ID, app.tabs[1].ID, app.tabs[2].ID, app.tabs[3].ID}
	if strings.Join(got, ",") != "settings,a,c,b" {
		t.Fatalf("id-relative order = %v", got)
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

func TestSOCKSMethodsContain(t *testing.T) {
	if !socksMethodsContain([]byte{0x02, 0x00}, 0x00) {
		t.Fatal("no-auth method not detected")
	}
	if socksMethodsContain([]byte{0x02}, 0x00) {
		t.Fatal("unexpected no-auth method")
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

	buf.Reset()
	buf.WriteByte(0)
	if _, _, err := readSOCKSAddr(buf, 0x03); err == nil {
		t.Fatal("empty domain accepted")
	}

	buf.Reset()
	buf.WriteByte(11)
	buf.WriteString("example.com")
	_ = binary.Write(buf, binary.BigEndian, uint16(0))
	if _, _, err := readSOCKSAddr(buf, 0x03); err == nil {
		t.Fatal("port 0 accepted")
	}
}

func TestValidateSOCKSTarget(t *testing.T) {
	if host, port, err := validateSOCKSTarget(" example.com ", 443); err != nil || host != "example.com" || port != 443 {
		t.Fatalf("valid target = %q %d %v", host, port, err)
	}
	for _, tc := range []struct {
		host string
		port uint16
	}{
		{"", 443},
		{"bad host", 443},
		{"example.com", 0},
	} {
		if _, _, err := validateSOCKSTarget(tc.host, tc.port); err == nil {
			t.Fatalf("validateSOCKSTarget(%q,%d) succeeded", tc.host, tc.port)
		}
	}
}
