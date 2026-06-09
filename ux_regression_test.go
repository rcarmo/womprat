package main

import (
	"os"
	"strings"
	"testing"
)

func readFileForRegression(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSettingsOnlyListsAdvertisedExitNodes(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	want := "peers.filter(p=>p.online && p.exitNodeOption)"
	if !strings.Contains(s, want) {
		t.Fatalf("settings exit-node picker must filter to advertised exit nodes; missing %q", want)
	}
	if strings.Contains(s, "peers.filter(p=>p.online).forEach") {
		t.Fatal("settings exit-node picker must not list every online peer")
	}
}

func TestBackendRejectsNonExitNodePeers(t *testing.T) {
	s := readFileForRegression(t, "settings_api.go")
	for _, want := range []string{
		"if !p.ExitNodeOption",
		"is not advertised as an exit node",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("backend exit-node validation missing %q", want)
		}
	}
}

func TestPeersExposeExitNodeOption(t *testing.T) {
	s := readFileForRegression(t, "main.go")
	for _, want := range []string{
		"ExitNodeOption bool",
		"ExitNodeOption: p.ExitNodeOption",
		"json:\"exitNodeOption\"",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("peer API missing exit-node option field fragment %q", want)
		}
	}
}

func TestNativeHostResizesEmbeddedShellWebView(t *testing.T) {
	wrapperCommon := readFileForRegression(t, "internal/go-webview2/common.go")
	wrapperImpl := readFileForRegression(t, "internal/go-webview2/webview.go")
	host := readFileForRegression(t, "native_content_windows.go")
	for _, tc := range []struct{ name, content, want string }{
		{"wrapper interface", wrapperCommon, "Resize()"},
		{"wrapper implementation", wrapperImpl, "func (w *webview) Resize()"},
		{"native host shell resize", host, "m.shell.Resize()"},
		{"native host shell hwnd", host, "shellHWND"},
	} {
		if !strings.Contains(tc.content, tc.want) {
			t.Fatalf("%s missing %q", tc.name, tc.want)
		}
	}
}

func TestSettingsAndBrowserActivationAreAwaited(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"window.openSettings = async function()",
		"if (window.womprat_openSettings) await womprat_openSettings();",
		"await activateTab(tabId, { skipNative: true });",
		"window.showBrowserTab = async function",
		"setBrowserStatus(id, `Loading ${navUrl}…`)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend missing async activation/status fragment %q", want)
		}
	}
}

func TestNoProgressStripInShell(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, forbidden := range []string{"id=\"url-progress\"", "urlProgressIndeterminate", "#url-progress"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("progress strip should remain disabled; found %q", forbidden)
		}
	}
	if !strings.Contains(s, "function setURLProgress(_active, _done = false)") {
		t.Fatal("setURLProgress should remain a no-op while native navigation is stabilized")
	}
}
