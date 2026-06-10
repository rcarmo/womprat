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

func TestSettingsStatusUsesSafeDOMConstruction(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"const safeKind = ['ok','error','warn','info'].includes",
		"span.textContent = String(text ?? '');",
		"el.appendChild(span);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings status sanitizer missing %q", want)
		}
	}
	if strings.Contains(s, "${kind}") {
		t.Fatal("settings status must not interpolate raw kind into innerHTML")
	}
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
	wrapperCommon := readFileForRegression(t, "../../internal/go-webview2/common.go")
	wrapperImpl := readFileForRegression(t, "../../internal/go-webview2/webview.go")
	host := readFileForRegression(t, "native_content_windows.go")
	for _, tc := range []struct{ name, content, want string }{
		{"wrapper interface", wrapperCommon, "Resize()"},
		{"wrapper implementation", wrapperImpl, "func (w *webview) Resize()"},
		{"native host shell resize", host, "m.shell.Resize()"},
		{"native host shell hwnd", host, "shellHWND"},
		{"native host destroy resets active", host, "wasActive := m.browserActive == tabID"},
	} {
		if !strings.Contains(tc.content, tc.want) {
			t.Fatalf("%s missing %q", tc.name, tc.want)
		}
	}
}

func TestSettingsActivationIsResilient(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"window.openSettings = function()",
		"if (window.womprat_openSettings) { try { womprat_openSettings(); } catch {} }",
		"activateTab(tabId, { skipNative: true });",
		"window.showBrowserTab = function",
		"setBrowserStatus(id, `Loading ${navUrl}…`)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend missing resilient activation/status fragment %q", want)
		}
	}
	if strings.Contains(s, "await womprat_switchTab") || strings.Contains(s, "await womprat_openSettings") {
		t.Fatal("activation must not await native bound calls; that can hang or abort the flow")
	}
}

func TestCustomSchemesUseSingleFrontendDispatcher(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function parseCustomURL(url)",
		"const defaults = { ssh: 22, vnc: 5900, rdp: 3389 }",
		"function openCustomViewerFallback(target)",
		"function callNativeCustomViewer(target, text)",
		"if (target.scheme === 'vnc' || target.scheme === 'rdp') return callNativeCustomViewer(target, text);",
		"if (openSpecialURL(url)) return;",
		"if (openSpecialURL(url)) return;\n  const navUrl = normalizeBrowserURL(url);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend custom scheme dispatcher missing %q", want)
		}
	}
	for _, forbidden := range []string{"const rdpMatch =", "const vncMatch =", "const sshMatch ="} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("frontend must not use per-scheme URL parser %q", forbidden)
		}
	}
}

func TestFrontendCentralizesBrowserURLNormalization(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	if !strings.Contains(s, "function normalizeBrowserURL(url)") {
		t.Fatal("frontend missing normalizeBrowserURL")
	}
	if strings.Contains(s, "navUrl = 'http://' + navUrl") || strings.Contains(s, "'http://' + url") {
		t.Fatal("frontend must not use ad-hoc browser URL prefixing")
	}
}

func TestFrontendValidatesDownloadURLs(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function downloadDisplayName(url)",
		"if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';",
		"Invalid download URL",
		"if (!start.ok)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend download validation missing %q", want)
		}
	}
}

func TestFrontendSanitizesBrowserMetadata(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"const MAX_BROWSER_TITLE_RUNES = 200",
		"const MAX_FAVICON_URL_BYTES = 2048",
		"function sanitizeBrowserTitle(value)",
		"function sanitizeFaviconURL(value)",
		"const cleanFavicon = sanitizeFaviconURL(favicon)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend browser metadata sanitizer missing %q", want)
		}
	}
}

func TestFrontendClampsTerminalDimensions(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"const MIN_TERMINAL_COLS = 20",
		"const MAX_TERMINAL_COLS = 500",
		"function clampTerminalCols(value)",
		"function clampTerminalRows(value)",
		"new URLSearchParams({ tab: String(sshTabId || ''), cols: String(cols), rows: String(rows), token: TOKEN })",
		"ws.send(JSON.stringify({type:'resize', cols: clampTerminalCols(cols), rows: clampTerminalRows(rows)}));",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend terminal clamp missing %q", want)
		}
	}
}

func TestFrontendValidatesTabIDsBeforeDOMUse(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function validTabID(id)",
		"function newLocalTabID(prefix)",
		"const tabId = validTabID(options.id) ? options.id : newLocalTabID('term');",
		"if (!t || t.id === 'settings' || !validTabID(t.id)) return null;",
		"filter(t => validTabID(t.id) &&",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend tab id validation missing %q", want)
		}
	}
}

func TestFrontendPersistsOnlySanitizedURLState(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function normalizeHistoryURL(url)",
		"return parsed.map(normalizeHistoryURL).filter(Boolean).slice(0, 100);",
		"function sanitizeTabForSave(t)",
		"const tabs = state.tabs.map(sanitizeTabForSave).filter(Boolean).slice(0, 100);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend persistence sanitizer missing %q", want)
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
