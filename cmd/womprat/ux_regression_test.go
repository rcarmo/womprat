package main

import (
	"os"
	"strings"
	"testing"
)

func TestBrowserURLBarSyncsFragmentNavigations(t *testing.T) {
	native := readFileForRegression(t, "native_content_windows.go")
	// The in-page bridge must report the live URL on same-document navigations
	// (fragment/hash and SPA pushState/popstate), which do not fire
	// NavigationCompleted.
	for _, want := range []string{
		"wompratURL: location.href",
		"window.addEventListener('hashchange', send);",
		"window.addEventListener('popstate', send);",
		"history.pushState = function()",
		"func parseURLMessage(raw string) string",
		"window.wompratSyncTabURL(%s,%s)",
	} {
		if !strings.Contains(native, want) {
			t.Fatalf("fragment URL sync missing %q", want)
		}
	}
}

func TestRDPViewportFillAndStableResize(t *testing.T) {
	rdpjs := readFileForRegression(t, "frontend/rdp.js")
	rdpgo := readFileForRegression(t, "rdp.go")
	// (a) Fit must be allowed to upscale so the canvas fills the viewport (no
	// letterbox); the old `,1` cap that prevented upscaling must be gone.
	if strings.Contains(rdpjs, "Math.min(Z.width/$,Z.height/J,1)||1") {
		t.Fatal("fitCanvas must allow upscaling to fill the viewport (remove the cap at 1)")
	}
	if !strings.Contains(rdpjs, "Math.min(Z.width/$,Z.height/J)||1") {
		t.Fatal("fitCanvas fill scaling missing")
	}
	// (c) Resize must not trigger a reconnect (which re-prompts credentials):
	// request DisplayControl for dynamic resize and neutralize reconnectWithNewSize.
	if !strings.Contains(rdpjs, `Q.searchParams.set("displayControl","true")`) {
		t.Fatal("RDP connect must request displayControl for dynamic resize")
	}
	if !strings.Contains(rdpjs, "this.client.reconnectWithNewSize=()=>{}") {
		t.Fatal("RDP wrapper must disable reconnect-on-resize to avoid re-auth prompts")
	}
	// (b) Negotiated encodings must be logged server-side for verification.
	if !strings.Contains(rdpgo, "rdp caps: colorDepth=") {
		t.Fatal("negotiated RDP capabilities must be logged")
	}
	if !strings.Contains(rdpgo, "rdp: requested %dx%d colorDepth=") {
		t.Fatal("requested RDP parameters must be logged")
	}
}

func readFileForRegression(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDownloadHandlerUsesSharedGetMethodGuard(t *testing.T) {
	s := readFileForRegression(t, "download.go")
	if !strings.Contains(s, "func (a *App) handleDownload") || !strings.Contains(s, "if !requireGET(w, r)") {
		t.Fatal("download handler should use shared GET method guard")
	}
	if !strings.Contains(s, "download: remove incomplete file") {
		t.Fatal("download cleanup failures should be logged")
	}
}

func TestSettingsDiagnosticsTabIsFirstWithRuntimeData(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	// Diagnostics must be the first tab button and the default-active panel.
	firstBtn := strings.Index(s, "data-tab=\"diagnostics\"")
	securityBtn := strings.Index(s, "data-tab=\"security\"")
	if firstBtn < 0 || securityBtn < 0 || firstBtn > securityBtn {
		t.Fatal("Diagnostics must be the first settings tab (before Security)")
	}
	if !strings.Contains(s, "<button class=\"tab-btn active\" data-tab=\"diagnostics\">") {
		t.Fatal("Diagnostics tab button must be default-active")
	}
	if !strings.Contains(s, "<div class=\"tab-panel active\" id=\"panel-diagnostics\">") {
		t.Fatal("Diagnostics panel must be default-active")
	}
	if strings.Contains(s, "<div class=\"tab-panel active\" id=\"panel-security\">") {
		t.Fatal("Security panel must no longer be default-active")
	}
	// Runtime/Storage/Debug-logging diagnostics data must live in the Diagnostics
	// panel, between its open and the next panel.
	diagStart := strings.Index(s, "id=\"panel-diagnostics\"")
	diagEnd := strings.Index(s, "id=\"panel-appearance\"")
	if diagStart < 0 || diagEnd < 0 || diagStart > diagEnd {
		t.Fatal("could not locate diagnostics panel bounds")
	}
	diagPanel := s[diagStart:diagEnd]
	for _, id := range []string{"about-ts-connected", "about-exit-node", "about-socks", "about-config-dir", "about-webview-data", "debug-log"} {
		if !strings.Contains(diagPanel, "id=\""+id+"\"") {
			t.Fatalf("diagnostics runtime field %q must be in the Diagnostics panel", id)
		}
	}
	// About panel keeps version info + links but no runtime diagnostics.
	aboutStart := strings.Index(s, "id=\"panel-about\"")
	if aboutStart < 0 {
		t.Fatal("about panel missing")
	}
	aboutPanel := s[aboutStart:]
	if !strings.Contains(aboutPanel, "id=\"about-version\"") {
		t.Fatal("About panel must keep version info")
	}
	if strings.Contains(aboutPanel, "id=\"about-ts-connected\"") || strings.Contains(aboutPanel, "id=\"debug-log\"") {
		t.Fatal("About panel must no longer contain runtime/diagnostics data")
	}
}

func TestSettingsUsesInAppModalNotNativeDialogs(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	// Native confirm()/alert()/prompt() show the internal "127.0.0.1:<port> says"
	// origin chrome, so settings must use the in-app modal instead.
	for _, banned := range []string{"confirm(", "alert(", "prompt("} {
		idx := 0
		for {
			j := strings.Index(s[idx:], banned)
			if j < 0 {
				break
			}
			pos := idx + j
			// Allow method-style calls (confirmDialog/promptDialog) and comment text.
			prev := byte(' ')
			if pos > 0 {
				prev = s[pos-1]
			}
			word := s[pos : pos+len(banned)]
			isIdentChar := prev == '.' || prev == 'm' || prev == 'D' // .confirm, confirmDialog, promptDialog
			lineStart := strings.LastIndexByte(s[:pos], '\n') + 1
			line := s[lineStart:pos]
			isComment := strings.Contains(line, "//")
			if !isIdentChar && !isComment {
				t.Fatalf("settings.html uses native dialog %q at offset %d (line: %q)", word, pos, strings.TrimSpace(s[lineStart:pos+40]))
			}
			idx = pos + len(banned)
		}
	}
	for _, want := range []string{
		"function confirmDialog(message",
		"function promptDialog(message",
		"id=\"app-modal\"",
		"await confirmDialog(",
		"await promptDialog(",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings in-app modal missing %q", want)
		}
	}
}

func TestSettingsMultiMethodHandlersUseHTTPMethodConstants(t *testing.T) {
	s := readFileForRegression(t, "settings_api.go")
	for _, want := range []string{
		"case http.MethodGet, http.MethodHead:",
		"case http.MethodPost:",
		"case http.MethodDelete:",
		"case http.MethodPatch:",
		"http.StatusMethodNotAllowed",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings multi-method handler should use method constant/status %q", want)
		}
	}
}

func TestReadOnlyHandlersUseSharedGetMethodGuard(t *testing.T) {
	mainSource := readFileForRegression(t, "main.go")
	loggingSource := readFileForRegression(t, "logging.go")
	for _, tc := range []struct{ name, source string }{
		{"frontend", mainSource},
		{"logs", loggingSource},
	} {
		if !strings.Contains(tc.source, "if !requireGET(w, r)") {
			t.Fatalf("%s handler should use shared GET method guard", tc.name)
		}
	}
	if !strings.Contains(loggingSource, "case http.MethodGet, http.MethodHead:") || !strings.Contains(loggingSource, "case http.MethodPost:") {
		t.Fatal("debug log handler should use http method constants")
	}
}

func TestShellEvalDispatchesToUIThread(t *testing.T) {
	s := readFileForRegression(t, "main.go")
	for _, want := range []string{
		"js := fmt.Sprintf(format, args...)",
		"a.onUIThread(func() { a.webview.Eval(js) })",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("shell eval UI dispatch missing %q", want)
		}
	}
	if strings.Contains(s, "a.webview.Eval(fmt.Sprintf(format, args...))") {
		t.Fatal("shell eval must not call WebView Eval directly from binding callbacks")
	}
}

func TestTailscaleStartupUsesBoundedContext(t *testing.T) {
	s := readFileForRegression(t, "main.go")
	for _, want := range []string{
		"tailscaleUpTimeout = 30 * time.Second",
		"context.WithTimeout(context.Background(), tailscaleUpTimeout)",
		"ts.Up(upCtx)",
		"defer cancelUp()",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Tailscale startup timeout guard missing %q", want)
		}
	}
	if strings.Contains(s, "ts.Up(context.Background())") {
		t.Fatal("Tailscale startup must not use an unbounded background context")
	}
}

func TestSOCKSRelayHalfClosesAndWaitsBothDirections(t *testing.T) {
	s := readFileForRegression(t, "socks.go")
	for _, want := range []string{
		"func halfCloseWrite(c net.Conn)",
		"CloseWrite() error",
		"wg.Wait()",
		"halfCloseWrite(remote)",
		"halfCloseWrite(conn)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SOCKS relay half-close handling missing %q", want)
		}
	}
	if strings.Contains(s, "done <- struct{}{} }()") {
		t.Fatal("SOCKS relay must not tear down both sockets on first direction completion")
	}
}

func TestAllTsnetDialsPreferIPv4(t *testing.T) {
	for _, f := range []string{"socks.go", "vnc.go", "rdp.go", "download.go", "main.go"} {
		s := readFileForRegression(t, f)
		if !strings.Contains(s, "dialTSNetPreferIPv4(") {
			t.Fatalf("%s should dial tsnet via dialTSNetPreferIPv4", f)
		}
	}
	for _, f := range []string{"vnc.go", "rdp.go", "download.go"} {
		s := readFileForRegression(t, f)
		if strings.Contains(s, "ts.Dial(") {
			t.Fatalf("%s must not call ts.Dial directly; use dialTSNetPreferIPv4", f)
		}
	}
}

func TestSOCKSDialPrefersIPv4(t *testing.T) {
	s := readFileForRegression(t, "socks.go")
	for _, want := range []string{
		"func dialTSNetPreferIPv4(ctx context.Context, ts *tsnet.Server, addr string)",
		"[]string{\"tcp4\", \"tcp6\", \"tcp\"}",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SOCKS IPv4-preferred dial missing %q", want)
		}
	}
}

func TestSOCKSConnectUsesBoundedTsnetDial(t *testing.T) {
	s := readFileForRegression(t, "socks.go")
	for _, want := range []string{
		"const socksDialTimeout = 10 * time.Second",
		"context.WithTimeout(context.Background(), socksDialTimeout)",
		"dialTSNetPreferIPv4(dialCtx, ts, addr)",
		"defer cancelDial()",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SOCKS dial timeout guard missing %q", want)
		}
	}
}

func TestSSHConnectUsesRequestScopedDialTimeout(t *testing.T) {
	s := readFileForRegression(t, "main.go")
	for _, want := range []string{
		"context.WithTimeout(r.Context(), 10*time.Second)",
		"dialTSNetPreferIPv4(dialCtx, ts, addr)",
		"defer cancelDial()",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SSH dial timeout/cancellation guard missing %q", want)
		}
	}
	if strings.Contains(s, "ts.Dial(context.Background(), \"tcp\", addr)") {
		t.Fatal("SSH handlers must not dial with context.Background")
	}
}

func TestStoredBrowserURLsUseSharedBrowserURLPolicy(t *testing.T) {
	s := readFileForRegression(t, "config.go")
	for _, want := range []string{
		"normalized, err := normalizeBrowserURL(raw)",
		"normalizedURL, err := normalizeBrowserURL(tab.URL)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("stored browser URL policy missing %q", want)
		}
	}
}

func TestConfigFallbacksAreDiagnosable(t *testing.T) {
	s := readFileForRegression(t, "config.go")
	for _, want := range []string{
		"config decrypt failed, using defaults",
		"config JSON decode failed, using defaults",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("config fallback diagnostic missing %q", want)
		}
	}
}

func TestAuthMiddlewareUsesConstantTimeTokenCompare(t *testing.T) {
	s := readFileForRegression(t, "main.go")
	if !strings.Contains(s, "subtle.ConstantTimeCompare([]byte(token), []byte(a.sessionToken))") {
		t.Fatal("auth middleware should compare session tokens in constant time")
	}
	if strings.Contains(s, "token != a.sessionToken") {
		t.Fatal("auth middleware must not use direct token string comparison")
	}
}

func TestMainHandlersUseSharedPostMethodGuard(t *testing.T) {
	s := readFileForRegression(t, "main.go")
	for _, want := range []string{
		"func (a *App) handleUnlock",
		"func (a *App) handleSSHConnect",
		"func (a *App) handleSSHAuthPassword",
		"if !requirePOST(w, r)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("main handler method guard missing %q", want)
		}
	}
}

func TestBrowserSettingsHandlersUseSharedPostMethodGuard(t *testing.T) {
	s := readFileForRegression(t, "browser_settings.go")
	for _, want := range []string{
		"func (a *App) handleClearCache",
		"func (a *App) handleSavePasswordsToggle",
		"if !requirePOST(w, r)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("browser settings method guard missing %q", want)
		}
	}
}

func TestSettingsHandlersUseSharedPostMethodGuard(t *testing.T) {
	helpers := readFileForRegression(t, "http_method.go")
	settings := readFileForRegression(t, "settings_api.go")
	if !strings.Contains(helpers, "func requirePOST(w http.ResponseWriter, r *http.Request) bool") {
		t.Fatal("shared POST method guard missing")
	}
	for _, want := range []string{
		"func (a *App) handleSetUnlockMethod",
		"if !requirePOST(w, r)",
		"func (a *App) handleSaveTabs",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("settings method guard missing %q", want)
		}
	}
}

func TestSettingsAuthKeyButtonsStayOnInputRow(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		".key-row{align-items:center}",
		".key-row input{margin-bottom:0}",
		"<div class=\"row key-row\">",
		"data-action=\"toggle-ts-key\">Show</button>\n      <button class=\"btn-primary\" data-action=\"save-tailscale\"",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings auth key layout missing %q", want)
		}
	}
}

func TestSettingsUsesFetchJSONHelper(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	if !strings.Contains(s, "async function fetchJSON(url, fallback)") {
		t.Fatal("settings missing fetchJSON helper")
	}
	if strings.Contains(s, ".then(r=>r.json()).catch") {
		t.Fatal("settings should use fetchJSON instead of duplicated then/catch fetch parsing")
	}
}

func TestSettingsHasNoEmptyCatchBlocks(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, forbidden := range []string{"catch {}", "catch(e){}", "catch (e) {}"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("settings must not contain empty catch block %q", forbidden)
		}
	}
	for _, want := range []string{
		"console.warn(`fetch JSON failed for ${url}`, err);",
		"console.warn('tailscale key response parse failed', err);",
		"console.warn('tailscale status refresh failed', err);",
		"Unlock method save failed: ${err.message}",
		"Password save failed: ${err.message}",
		"Tailscale key save failed: ${err.message}",
		"Tailscale disconnect failed: ${err.message}",
		"Exit node save failed: ${err.message}",
		"Appearance save failed: ${err.message}",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings failure reporting missing %q", want)
		}
	}
	if !strings.Contains(s, "id=\"browser-status\"") {
		t.Fatal("settings browser status area missing")
	}
}

func TestSettingsHasNoInlineEventHandlers(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, forbidden := range []string{"onclick=", "onchange=", "onerror="} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("settings must not contain inline event handler %q", forbidden)
		}
	}
}

func TestSettingsRemainingActionsUseDataAttributes(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"data-action=\"save-master-password\"",
		"data-action=\"refresh-hosts\"",
		"data-action=\"clear-cache\"",
		"data-action=\"save-appearance\"",
		"document.getElementById('exit-node')?.addEventListener('change'",
		"document.getElementById('debug-log')?.addEventListener('change'",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings remaining action wiring missing %q", want)
		}
	}
	for _, forbidden := range []string{"onchange=\"setExitNode", "onclick=\"refreshHosts()", "onclick=\"clearCache()", "onclick=\"saveAppearance()", "onchange=\"setDebugLog"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("settings action must not use inline handler %q", forbidden)
		}
	}
}

func TestSettingsTailscaleAndKeyActionsUseDataAttributes(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"data-action=\"toggle-ts-key\"",
		"data-action=\"save-tailscale\"",
		"data-action=\"import-key\"",
		"data-action=\"generate-key\"",
		"addEventListener('click', saveTailscale)",
		"addEventListener('change', ev => handleKeyFile(ev.currentTarget))",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings action data-attribute wiring missing %q", want)
		}
	}
	for _, forbidden := range []string{"onclick=\"toggleVis('ts-key')", "onclick=\"saveTailscale()", "onclick=\"importKey()", "onclick=\"generateKey()", "onchange=\"handleKeyFile"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("settings action must not use inline handler %q", forbidden)
		}
	}
}

func TestSettingsUnlockOptionsUseDataAttributes(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"data-unlock=\"dpapi\"",
		"data-unlock=\"master\"",
		"document.querySelectorAll('.option[data-unlock]').forEach",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings unlock data-attribute wiring missing %q", want)
		}
	}
	if strings.Contains(s, "onclick=\"selectUnlock") {
		t.Fatal("settings unlock options must not use inline selectUnlock handlers")
	}
}

func TestSettingsTabsUseDataAttributes(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"data-tab=\"security\"",
		"document.querySelectorAll('.tab-btn[data-tab]').forEach",
		"b.classList.toggle('active', b.dataset.tab === id)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings tab data-attribute wiring missing %q", want)
		}
	}
	if strings.Contains(s, "onclick=\"showTab") {
		t.Fatal("settings tab buttons must not use inline showTab handlers")
	}
}

func TestSettingsExitNodeSelectUsesSafeDOMConstruction(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"sel.textContent = '';",
		"const none = document.createElement('option');",
		"none.textContent = 'None — tailnet only';",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings exit-node select DOM rendering missing %q", want)
		}
	}
	if strings.Contains(s, "sel.innerHTML = '<option") {
		t.Fatal("settings exit-node select must not use innerHTML")
	}
}

func TestSettingsDiagnosticsPanelCoversNetworkChecks(t *testing.T) {
	settings := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"data-tab=\"diagnostics\"",
		"id=\"panel-diagnostics\"",
		"async function runDiagnostics()",
		"fetch('/api/settings/diagnostics')",
		"diagnostics-grid",
		"google.com through SOCKS",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("settings diagnostics UI missing %q", want)
		}
	}
	// Diagnostics auto-runs on open (no manual Run button).
	if strings.Contains(settings, "data-action=\"run-diagnostics\"") {
		t.Fatal("Diagnostics must auto-run on open, not via a Run button")
	}
	if !strings.Contains(settings, "if (id === 'diagnostics' && typeof runDiagnostics === 'function') runDiagnostics();") {
		t.Fatal("Diagnostics must run when its tab is opened")
	}
	api := readFileForRegression(t, "settings_api.go")
	if !strings.Contains(api, "a.handleDiagnostics") {
		t.Fatal("settings diagnostics route missing")
	}
	diag := readFileForRegression(t, "diagnostics.go")
	for _, want := range []string{
		"func (a *App) handleDiagnostics",
		"Tailscale connectivity",
		"SOCKS listener",
		"SOCKS DNS/connect to google.com",
		"diagnoseSOCKSDomainConnect",
		"google.com",
		"byte(len(host))",
	} {
		if !strings.Contains(diag, want) {
			t.Fatalf("diagnostics backend missing %q", want)
		}
	}
}

func TestSettingsReportsAsyncOperationFailures(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"setStatus('ts-status', 'error', 'Enter an auth key')",
		"if (!res.ok) { setStatus('exit-status', 'error', await res.text()); await loadExitNodes(); return; }",
		"if (!res.ok) { setStatus('debug-status', 'error', await res.text()); return; }",
		"if (!res.ok) { setStatus('pw-status', 'error', await res.text()); await loadConfigSettings(); }",
		"id=\"debug-status\"",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings async error reporting missing %q", want)
		}
	}
}

func TestSettingsBoundsSSHKeyImportSize(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"const MAX_SSH_KEY_BYTES = 64 * 1024;",
		"file.size > MAX_SSH_KEY_BYTES",
		"SSH key file too large",
		"Could not read key file",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings SSH key size guard missing %q", want)
		}
	}
}

func TestSettingsValidatesKeyAndHostInputs(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"function safeSSHKeyName(name)",
		"function safeHostName(host)",
		"function safeHostURL(url)",
		"async function postSSHKeyJSON(url, body)",
		"SSH key action failed: ${err.message}",
		"SSH key delete failed: ${err.message}",
		"const safeURL = safeHostURL(url);",
		"window.parent.postMessage({type: 'womprat-open-browser', url: safeURL, title: safeURL}, window.location.origin);",
		"window.location.href = safeURL",
		"setStatus('keys-status', 'error', 'Invalid key name')",
		"setStatus('keys-status', 'ok', 'Key imported')",
		"setStatus('keys-status', 'ok', 'Key generated')",
		"setStatus('hosts-status', 'error', 'Invalid host')",
		"setStatus('hosts-status', 'error', 'Invalid browser URL')",
		"setStatus('hosts-status', 'ok', 'Host updated')",
		"Host update failed: ${err.message}",
		"if (!res.ok) { setStatus('hosts-status', 'error', await res.text()); return false; }",
		"id=\"keys-status\"",
		"id=\"hosts-status\"",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings input validation missing %q", want)
		}
	}
}

func TestSettingsBrowserActionsReportFailures(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"async function postBrowserAction(url, options = {})",
		"if (!res.ok) { setStatus('browser-status', 'error', await res.text()); return false; }",
		"Browser data action failed: ${err.message}",
		"Save passwords toggle failed: ${err.message}",
		"setStatus('browser-status', 'ok', 'Browser data updated');",
		"if (!res.ok) { setStatus('browser-status', 'error', await res.text()); await loadBrowserSettings(); }",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings browser action error handling missing %q", want)
		}
	}
}

func TestSettingsBrowserTablesUseSafeDOMConstruction(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"clear.onclick = () => clearCookiesFor(c.domain || '');",
		"clear.onclick = () => deletePassword(p.site || '');",
		"tr.append(domain, count, actions);",
		"tr.append(site, username, actions);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings browser table DOM rendering missing %q", want)
		}
	}
	for _, forbidden := range []string{"onclick=\"clearCookiesFor", "onclick=\"deletePassword"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("settings browser tables must not template inline handler %q", forbidden)
		}
	}
}

func TestSettingsOpenHostUsesParentMessageBridge(t *testing.T) {
	settings := readFileForRegression(t, "frontend/settings.html")
	shell := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"type: 'womprat-open-browser'",
		"window.parent.postMessage",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("settings host open bridge missing %q", want)
		}
	}
	for _, want := range []string{
		"if (e.origin !== window.location.origin) return;",
		"e.data?.type === 'womprat-open-browser'",
		"openBrowser(e.data.url, e.data.title || e.data.url);",
	} {
		if !strings.Contains(shell, want) {
			t.Fatalf("shell host open bridge missing %q", want)
		}
	}
}

func TestSettingsHostsTableUsesSafeDOMConstruction(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"input.onchange = () => updateHost(p.name, 'url', input.value);",
		"open.onclick = () => openHostURL(input.value);",
		"tr.append(name, urlCell, status, actions);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings hosts DOM rendering missing %q", want)
		}
	}
	for _, forbidden := range []string{"onchange=\"updateHost", "onclick=\"openHostURL"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("settings hosts table must not template inline handler %q", forbidden)
		}
	}
}

func TestSettingsKeysTableUsesSafeDOMConstruction(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"function emptyTableRow(tbody, cols, text)",
		"btn.onclick = () => deleteKey(k.name || '');",
		"tr.append(name, fp, hosts, actions);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings keys DOM rendering missing %q", want)
		}
	}
	if strings.Contains(s, "onclick=\"deleteKey") {
		t.Fatal("settings keys table must not template onclick handlers")
	}
}

func TestSettingsStatusUsesSafeDOMConstruction(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"const requestedKind = String(kind || '');",
		"const safeKind = requestedKind === 'err' ? 'error'",
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

func TestSettingsLoadsSavedConfigValues(t *testing.T) {
	s := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"async function loadConfigSettings()",
		"fetchJSON('/api/settings/config', {})",
		"applyUnlockSelection(cfg.unlockMethod)",
		"fontSize.value = cfg.fontSize || 14",
		"restoreTabs.checked = !!cfg.restoreTabs",
		"autoConnect.checked = !!cfg.autoConnect",
		"await loadConfigSettings();",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings config loader missing %q", want)
		}
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

func TestShellUsesWindowQualifiedNativeBindings(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, forbidden := range []string{
		" womprat_newBrowser(",
		" womprat_navigate(",
		" womprat_openSettings(",
		" womprat_getTabs(",
		" womprat_switchTab(",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("native binding call must be window-qualified: %q", forbidden)
		}
	}
	for _, want := range []string{
		"window.womprat_newBrowser(navUrl)",
		"native browser open failed",
		"await window.womprat_getTabs()",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("window-qualified native binding missing %q", want)
		}
	}
}

func TestShellModuleHasLocalBindingsForWindowEntrypoints(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function callWindowFunction(name, args)",
		"function newBlankTab(...args) { return callWindowFunction('newBlankTab', args); }",
		"function openSettings(...args) { return callWindowFunction('openSettings', args); }",
		"function openBrowser(...args) { return callWindowFunction('openBrowser', args); }",
		"function navigateFromBar(...args) { return callWindowFunction('navigateFromBar', args); }",
		"function saveKey(...args) { return callWindowFunction('saveKey', args); }",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("shell module window entrypoint binding missing %q", want)
		}
	}
}

func TestSettingsActivationIsResilient(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"window.openSettings = function()",
		"if (window.womprat_openSettings) { try { window.womprat_openSettings(); } catch (err) { console.warn('native settings open failed', err); } }",
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

func TestCustomSchemeURLsNeverFallThroughToHTTP(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function coerceCustomScheme(text)",
		"replace(/^(ssh|vnc|rdp):(?!\\/\\/)/i",
		"function isCustomSchemeURL(text)",
		"if (isCustomSchemeURL(text)) {",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("custom scheme http-fallthrough guard missing %q", want)
		}
	}
}

func TestCustomSchemesUseSingleFrontendDispatcher(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function parseCustomURL(url)",
		"function decodeURLComponent(value)",
		"if (user === null || !validCustomHost(host)",
		"const defaults = { ssh: 22, vnc: 5900, rdp: 3389 }",
		"function openCustomViewerFallback(target)",
		"function callNativeCustomViewer(target, text)",
		"openCustomViewerFallback(target);\n  return true;",
		"if (target.scheme === 'vnc' || target.scheme === 'rdp') return callNativeCustomViewer(target, text);",
		"if (openSpecialURL(url)) return;",
		"const navUrl = normalizeBrowserURL(url);",
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

func TestRemoteDisplayNegotiationDefaultsPreferPerformance(t *testing.T) {
	vnc := readFileForRegression(t, "frontend/vnc.js")
	for _, want := range []string{
		"return [0, 5, 1, 16, 2, 4, -239, -307, -224, -223, -308];",
		"if (values.length > 0)\n    return values;",
	} {
		if !strings.Contains(vnc, want) {
			t.Fatalf("VNC compatibility encoding order missing %q", want)
		}
	}
	rdp := readFileForRegression(t, "rdp.go")
	for _, want := range []string{
		"enableRFX := !rdpQueryDisabled(r.URL.Query().Get(\"rfx\")) && !rdpQueryEnabled(r.URL.Query().Get(\"compat\"))",
		"func rdpQueryDisabled(raw string) bool",
		"case \"0\", \"false\", \"no\", \"off\", \"disable\", \"disabled\":",
	} {
		if !strings.Contains(rdp, want) {
			t.Fatalf("RDP performance negotiation default missing %q", want)
		}
	}
}

func TestRDPCredentialsUseDialogAndStatusbar(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"<div class=\"rdp-statusbar\" aria-live=\"polite\"><span class=\"rdp-status\" data-rdp-status>Preparing RDP…</span></div>",
		"<div class=\"rdp-dialog\" role=\"dialog\" aria-label=\"RDP credentials\">",
		"<div class=\"rdp-dialog-title\">Connect to RDP</div>",
		"data-rdp-user placeholder=\"User\"",
		"data-rdp-password type=\"password\"",
		"data-rdp-connect",
		"<div class=\"rdp-config\" hidden aria-hidden=\"true\">",
		"<span data-rdp-caps>codec status pending</span>",
		"<select data-rdp-depth>",
		"<button data-rdp-scale aria-pressed=\"true\">Fit</button>",
		"function updateRemoteTabTitle(tabId, title)",
		"function installRemoteTitleSync(root, tabId, type, fallbackURL)",
		"function installRDPPanelState(root)",
		"root.dataset.connecting = '1';",
		"delete root.dataset.connected;",
		"root.hasAttribute('data-busy') || /connecting|negotiating/.test(text)",
		"new MutationObserver(sync).observe(root, { attributes: true, attributeFilter: ['data-busy'] });",
		"enter rdp username|enter rdp password",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("RDP dialog/statusbar structure missing %q", want)
		}
	}
	if strings.Contains(s, "<div class=\"rdp-toolbar\">") {
		t.Fatal("RDP toolbar should be replaced by dialog/statusbar")
	}
}

func TestNavigationResultReportedToShell(t *testing.T) {
	native := readFileForRegression(t, "native_content_windows.go")
	for _, want := range []string{
		"ok := args != nil && args.IsSuccess()",
		"code = args.WebErrorStatus()",
		"webErrorStatusMessage(code, connected)",
		"window.wompratNavigationDone(%s,%s,%t,%d,%s)",
		"tsConnected func() bool",
	} {
		if !strings.Contains(native, want) {
			t.Fatalf("native navigation result wiring missing %q", want)
		}
	}
	args := readFileForRegression(t, "../../internal/go-webview2/pkg/edge/ICoreWebView2NavigationCompletedEventArgs.go")
	for _, want := range []string{
		"func (i *ICoreWebView2NavigationCompletedEventArgs) IsSuccess() bool",
		"func (i *ICoreWebView2NavigationCompletedEventArgs) WebErrorStatus() int32",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("navigation completed args accessor missing %q", want)
		}
	}
	gui := readFileForRegression(t, "gui_windows.go")
	if !strings.Contains(gui, "contentViews.tsConnected = app.tailscaleConnected") {
		t.Fatal("content manager tsConnected closure not wired")
	}
}

func TestNavigationStatusSurfaceLivesInChrome(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"<div id=\"nav-status\" role=\"status\" aria-live=\"polite\" hidden>",
		"id=\"nav-status-reload\"",
		"id=\"nav-status-settings\"",
		"id=\"nav-status-dismiss\"",
		"#nav-status{display:flex",
		"#nav-status.error{background:rgba(248,81,73,.12)",
		"function applyNavStatus(kind, message, tabId)",
		"function clearNavStatus()",
		"function setURLProgress(active, _done = false)",
		"applyNavStatus('loading'",
		"const useStatus = status && !status.hidden;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("navigation status chrome surface missing %q", want)
		}
	}
	if strings.Contains(s, "Disabled for now: native WebView2 navigation events can arrive out of phase") {
		t.Fatal("setURLProgress must no longer be a no-op")
	}
}

func TestNavigationDoneHandlesSuccessAndFailure(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"window.wompratNavigationDone = function(tabId, url, ok, errorCode, errorMessage)",
		"const succeeded = arguments.length < 3 ? true : !!ok;",
		"applyNavStatus('error', message, tabId);",
		"if (navStatusState.tabId && navStatusState.tabId !== id) clearNavStatus();",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("navigation done success/failure handling missing %q", want)
		}
	}
}

func TestURLBarSelectAllAndNoCompletionPopup(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	// The persistent native datalist completion popup is removed and browser
	// autofill suppressed.
	if strings.Contains(s, "id=\"url-input\" list=\"url-history\"") {
		t.Fatal("url-input must not bind the always-on datalist completion popup")
	}
	if !strings.Contains(s, "<input id=\"url-input\" autocomplete=\"off\"") {
		t.Fatal("url-input must disable browser autocomplete")
	}
	// Address-bar select-all on first click.
	for _, want := range []string{
		"let selectAllPending = false;",
		"urlInput.addEventListener('focus', () => { selectAllPending = true; });",
		"if (document.activeElement === urlInput) selectAllPending = false;",
		"urlInput.select();",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("URL-bar select-all behavior missing %q", want)
		}
	}
}

func TestURLBarNavigatesInPlaceAndSyncsLiveURL(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	native := readFileForRegression(t, "native_content_windows.go")
	chromium := readFileForRegression(t, "../../internal/go-webview2/pkg/edge/chromium.go")

	// URL bar acts like a real address bar: in-place navigation on a browser tab.
	for _, want := range []string{
		"const active = activeTabObj();",
		"active.type === 'browser' && window.womprat_navigate",
		"window.womprat_navigate(url); return;",
		"function openSpecialURLPreview(url)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("in-place URL-bar navigation missing %q", want)
		}
	}

	// Live URL sync from the native view back into the shell address bar/tab.
	for _, want := range []string{
		"window.wompratSyncTabURL = function(tabId, url)",
		"if (input && document.activeElement !== input) input.value = url;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("live URL sync missing %q", want)
		}
	}
	if !strings.Contains(chromium, "func (e *Chromium) GetSource() string") {
		t.Fatal("edge package must expose GetSource to read the live document URL")
	}
	for _, want := range []string{
		"src := cv.edge.GetSource();",
		"window.wompratSyncTabURL(%s,%s)",
	} {
		if !strings.Contains(native, want) {
			t.Fatalf("native navigation-completed URL sync missing %q", want)
		}
	}
}

func TestBrowserUXAuditFixesArePresent(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	native := readFileForRegression(t, "native_content_windows.go")
	chromium := readFileForRegression(t, "../../internal/go-webview2/pkg/edge/chromium.go")
	gui := readFileForRegression(t, "gui_windows.go")
	socks := readFileForRegression(t, "socks.go")

	// Issue 4: id-relative reorder semantics.
	if !strings.Contains(s, "window.womprat_reorderTab(fromId, beforeId)") {
		t.Fatal("Issue 4: shell must pass destination tab id, not index")
	}
	if !strings.Contains(gui, "func(tabID, beforeID string) { app.reorderTab(tabID, beforeID) }") {
		t.Fatal("Issue 4: reorder binding must take before id")
	}

	// Issue 5: retry-friendly dedupe.
	if !strings.Contains(s, "now - lastURLBarNavigation.at < 150") {
		t.Fatal("Issue 5: URL bar dedupe window must be reduced for retries")
	}
	if strings.Contains(s, "now - lastURLBarNavigation.at < 750") {
		t.Fatal("Issue 5: stale 750ms dedupe window still present")
	}

	// Issue 6: tsnet pre-flight.
	for _, want := range []string{
		"async function preflightConnectivity(tabId)",
		"ns.tsConnected === false",
		"preflightConnectivity(id);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Issue 6: connectivity pre-flight missing %q", want)
		}
	}
	if !strings.Contains(gui, "\"tsConnected\": app.tsServer != nil") {
		t.Fatal("Issue 6: getNetworkState must expose tsConnected")
	}

	// Issue 7: SOCKS failure classification.
	for _, want := range []string{
		"func socksReplyForDialError(err error) byte",
		"writeSOCKSReply(conn, socksReplyNetworkUnreach)",
		"writeSOCKSReply(conn, socksReplyForDialError(err))",
	} {
		if !strings.Contains(socks, want) {
			t.Fatalf("Issue 7: SOCKS reply classification missing %q", want)
		}
	}

	// Issue 8: native back/forward + can-go.
	for _, want := range []string{
		"func (e *Chromium) GoBack()",
		"func (e *Chromium) GoForward()",
		"func (e *Chromium) CanGoBack() bool",
		"func (e *Chromium) CanGoForward() bool",
	} {
		if !strings.Contains(chromium, want) {
			t.Fatalf("Issue 8: native nav wrapper missing %q", want)
		}
	}
	if strings.Contains(native, "history.back()") || strings.Contains(native, "history.forward()") {
		t.Fatal("Issue 8: native view must not use history.back()/forward() eval")
	}
	if !strings.Contains(native, "v.edge.GoBack()") || !strings.Contains(native, "v.edge.GoForward()") {
		t.Fatal("Issue 8: native view must call edge.GoBack/GoForward")
	}

	// Issue 9: nav button availability.
	for _, want := range []string{
		"window.wompratSetNavState = function(tabId, canBack, canForward)",
		"function updateNavButtonsUI()",
		"back.disabled = !(browser && sameTab && navAvailability.back)",
		"reload.disabled = !browser",
		"v.reportNavState()",
	} {
		if !strings.Contains(s+native, want) {
			t.Fatalf("Issue 9: nav button availability missing %q", want)
		}
	}

	// Issue 10: per-tab loading spinner.
	for _, want := range []string{
		"const loadingTabs = new Set();",
		"function setTabLoading(tabId, loading)",
		"if (loadingTabs.has(tab.id)) {",
		".tab-spinner{",
		"setTabLoading(id, true);",
		"setTabLoading(tabId, false);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Issue 10: per-tab spinner missing %q", want)
		}
	}

	// Issue 11: bounded legacy iframe purge.
	for _, want := range []string{
		"let firstExternal = '';",
		"if (!firstExternal) firstExternal = src;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Issue 11: bounded iframe purge missing %q", want)
		}
	}
	if strings.Contains(s, "if (window.womprat_newBrowser) window.womprat_newBrowser(src);") {
		t.Fatal("Issue 11: per-iframe navigation must be replaced by single navigation")
	}

	// Issue 12: URL-bar mode/hint (custom schemes only; no HTTP/HTTPS pill).
	for _, want := range []string{
		"id=\"url-mode\"",
		"function updateURLMode(tab)",
		"const URL_MODE_LABELS = { ssh: 'SSH', vnc: 'VNC', rdp: 'RDP', settings: 'CFG' };",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Issue 12: URL-bar mode hint missing %q", want)
		}
	}
	if strings.Contains(s, "label = u.protocol === 'https:' ? 'HTTPS' : 'HTTP';") {
		t.Fatal("Issue 12: HTTP/HTTPS pill must not be shown for browser tabs")
	}
}

func TestRemoteDisplayCanvasesUseSmoothScaling(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		".vnc-panel{position:relative;flex:1;display:grid;grid-template-rows:minmax(0,1fr);",
		".rdp-panel{position:relative;flex:1;display:grid;grid-template-rows:minmax(0,1fr);",
		".vnc-toolbar{display:none!important}",
		".vnc-viewport,.rdp-viewport{position:relative;z-index:1;min-height:0;min-width:0;display:grid;place-items:center;overflow:hidden;contain:layout paint",
		".rdp-statusbar{position:absolute;z-index:4;left:0;bottom:0;width:max-content;max-width:calc(100% - 2rem);height:2.5em;background:var(--surface-solid);border:1px solid var(--border);border-left:0;border-bottom:0;display:inline-flex;align-items:center;padding:0 1rem;gap:.8rem;font-size:.82em",
		".rdp-status::before{content:none!important}",
		".rdp-viewport{display:none}.rdp-panel[data-connecting] .rdp-viewport,.rdp-panel[data-connected] .rdp-viewport{display:grid}",
		".vnc-viewport canvas,.rdp-viewport canvas{display:block;box-sizing:border-box;max-width:100%;max-height:100%;width:auto;height:auto;object-fit:contain;image-rendering:auto",
		".vnc-viewport canvas:focus,.rdp-viewport canvas:focus{border:0!important;box-shadow:none!important;outline:0!important}",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("remote display canvas smooth scaling missing %q", want)
		}
	}
	if strings.Contains(s, "image-rendering:pixelated") {
		t.Fatal("remote display canvases must not force pixelated scaling")
	}
}

func TestVNCKeyboardMappingCoversKeypadAndReleasesModifiers(t *testing.T) {
	s := readFileForRegression(t, "frontend/vnc.js")
	for _, want := range []string{
		"Meta: 65511",
		"MetaLeft: 65511",
		"MetaRight: 65512",
		"OS: 65511",
		"OSRight: 65512",
		"Super: 65515",
		"var KEYSYM_BY_NUMPAD_CODE = {",
		"Numpad0: 65456",
		"NumpadEnter: 65421",
		"for (let i2 = 1;i2 <= 24; i2 += 1)",
		"KeyboardEvent.DOM_KEY_LOCATION_NUMPAD",
		"releaseActiveKeys()",
		"this.releaseActiveKeys();\n    try { this.ws?.close(1000, \"reconnect\")",
		"window.removeEventListener(\"blur\", this.windowBlurHandler)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("VNC keyboard mapping/release missing %q", want)
		}
	}
}

func TestVNCSessionControlsGateOnConnection(t *testing.T) {
	s := readFileForRegression(t, "frontend/vnc.js")
	for _, want := range []string{
		"setSessionControlsEnabled(enabled)",
		"this.setSessionControlsEnabled(false);",
		"this.setSessionControlsEnabled(true);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("VNC session control gating missing %q", want)
		}
	}
}

func TestRemoteDisplayWebSocketsSendAuthToken(t *testing.T) {
	vnc := readFileForRegression(t, "frontend/vnc.js")
	rdp := readFileForRegression(t, "frontend/rdp.js")
	if !strings.Contains(vnc, `url.searchParams.set("token"`) {
		t.Fatal("VNC websocket must include auth token")
	}
	if !strings.Contains(rdp, `searchParams.set("token"`) {
		t.Fatal("RDP websocket must include auth token")
	}
}

func TestVNCDesktopNameIsBounded(t *testing.T) {
	s := readFileForRegression(t, "frontend/vnc.js")
	for _, want := range []string{
		"var MAX_VNC_DESKTOP_NAME_CHARS = 200;",
		"function boundedVncDesktopName(text)",
		"this.serverName = boundedVncDesktopName(bytesToAscii(this.consume(nameLength)));",
		"this.serverName = boundedVncDesktopName(new TextDecoder().decode(nameBytes));",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("VNC desktop-name bound missing %q", want)
		}
	}
}

func TestVNCCursorIsBounded(t *testing.T) {
	s := readFileForRegression(t, "frontend/vnc.js")
	for _, want := range []string{
		"var MAX_VNC_CURSOR_DIMENSION = 256;",
		"var MAX_VNC_CURSOR_PIXELS = 256 * 256;",
		"rect.width > MAX_VNC_CURSOR_DIMENSION || rect.height > MAX_VNC_CURSOR_DIMENSION || rect.width * rect.height > MAX_VNC_CURSOR_PIXELS",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("VNC cursor bound missing %q", want)
		}
	}
}

func TestVNCFramebufferIsBounded(t *testing.T) {
	s := readFileForRegression(t, "frontend/vnc.js")
	for _, want := range []string{
		"var MAX_VNC_FRAMEBUFFER_DIMENSION = 8192;",
		"var MAX_VNC_FRAMEBUFFER_PIXELS = 16 * 1024 * 1024;",
		"throw new Error(`VNC framebuffer too large: ${w}×${h}`);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("VNC framebuffer bound missing %q", want)
		}
	}
}

func TestVNCClipboardIsBounded(t *testing.T) {
	s := readFileForRegression(t, "frontend/vnc.js")
	for _, want := range []string{
		"var MAX_VNC_CLIPBOARD_CHARS = 256 * 1024;",
		"var MAX_VNC_PASSWORD_CHARS = 8;",
		"function boundedVncClipboardText(text)",
		"function boundedVncPasswordText(text)",
		"return value ? boundedVncPasswordText(value) : null;",
		"this.send(clientCutText(text));",
		"input.value = boundedVncClipboardText(text);",
		"input.value = boundedVncClipboardText(event.text || \"\");",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("VNC clipboard bound missing %q", want)
		}
	}
}

func TestRemoteDisplayModuleLoadFailuresAreRecoverable(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"import('./vnc.js')",
		"delete root.dataset.started; const status = root.querySelector('[data-vnc-status]');",
		"VNC load failed:",
		"import('./rdp.js')",
		"delete root.dataset.started; const status = root.querySelector('[data-rdp-status]');",
		"RDP load failed:",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("remote display module failure handling missing %q", want)
		}
	}
}

func TestShellHasNoEmptyCatchBlocks(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	if strings.Contains(s, "catch {}") {
		t.Fatal("shell must not contain empty catch blocks")
	}
	if !strings.Contains(s, "function safeURL(text)") {
		t.Fatal("shell should use safeURL for expected URL parse failures")
	}
}

func TestNativeWebViewBridgeHasNoEmptyCatchBlocks(t *testing.T) {
	s := readFileForRegression(t, "native_content_windows.go")
	for _, forbidden := range []string{"catch(e){}", "catch (e) {}"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("native WebView bridge must not contain empty catch block %q", forbidden)
		}
	}
	if !strings.Contains(s, "reportBridgeError") {
		t.Fatal("native WebView bridge should report swallowed bridge failures")
	}
}

func TestVNCViewerHasNoEmptyCatchBlocks(t *testing.T) {
	s := readFileForRegression(t, "frontend/vnc.js")
	for _, forbidden := range []string{"catch {}", "catch(e){}", "catch (e) {}"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("VNC viewer must not contain empty catch block %q", forbidden)
		}
	}
	if !strings.Contains(s, "reportVncNonFatalError") {
		t.Fatal("VNC viewer should report non-fatal swallowed failures")
	}
}

func TestSetupAuthReportsFailures(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"Auth status unavailable:",
		"if (data.hasKey) {",
		"document.getElementById('setup').classList.add('hidden');",
		"if (!res.ok) throw new Error(await res.text());",
		"status.textContent = await res.text() || 'Unlock failed';",
		"Unlock failed: ${err.message}",
		"const data = await res.clone().json().catch(async () => ({error: await res.text()}));",
		"Connection failed: ${err.message}",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("setup auth failure reporting missing %q", want)
		}
	}
}

func TestShellGuardsSessionStorageAccess(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function readShellContentZoom()",
		"shell zoom storage unavailable",
		"function saveShellContentZoom(value)",
		"shell zoom storage save failed",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("shell zoom storage guard missing %q", want)
		}
	}
}

func TestShellLogsNativeAndCleanupFailures(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"console.warn('native tab switch failed'",
		"console.warn('native settings open failed'",
		"console.warn('native tab registration failed'",
		"console.warn('save open tabs failed'",
		"console.warn('rdp disconnect failed'",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("shell failure logging missing %q", want)
		}
	}
}

func TestTerminalDoesNotSwallowUnimplementedSearchShortcut(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	if strings.Contains(s, "TODO: show search bar UI") || strings.Contains(s, "ev.key === 'F'") {
		t.Fatal("terminal must not swallow Ctrl+Shift+F for an unimplemented search UI")
	}
	if !strings.Contains(s, "console.warn('terminal WebGL addon unavailable'") {
		t.Fatal("terminal WebGL addon fallback should be logged")
	}
}

func TestShellBrowserStatusUsesTextContent(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function setBrowserStatus(tabId, message)",
		"placeholder.textContent = String(message || '');",
		"panel.appendChild(placeholder);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("shell browser status DOM rendering missing %q", want)
		}
	}
	if strings.Contains(s, `browser-placeholder">${escapeHTML`) {
		t.Fatal("browser status must not use dynamic innerHTML")
	}
}

func TestShellFaviconRenderingUsesDOMEvents(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function tabIconElement(tab)",
		"img.addEventListener('error'",
		"el.appendChild(tabIconElement(tab));",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("shell favicon DOM rendering missing %q", want)
		}
	}
	if strings.Contains(s, "onerror=") {
		t.Fatal("shell must not use inline favicon onerror handlers")
	}
}

func TestShellReferencedHelpersAreDefined(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	// Identifiers used by the shell that must remain defined; a missing one throws
	// at runtime and silently breaks navigation/history.
	for _, name := range []string{"isHistoryURL", "normalizeHistoryURL", "refreshURLHistoryDatalist", "normalizeBrowserURL", "parseCustomURL", "openBrowser", "navigateFromBar"} {
		if !strings.Contains(s, "function "+name) && !strings.Contains(s, "window."+name+" = function") {
			t.Fatalf("shell references %q but it is not defined", name)
		}
	}
}

func TestURLBarEnterIsSingleNavigation(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"event.preventDefault();\n    event.stopPropagation();\n    if (event.repeat) return;\n    window.navigateFromBar();",
		"let lastURLBarNavigation = { url: '', at: 0 };",
		"if (lastURLBarNavigation.url === url && now - lastURLBarNavigation.at < 150) return;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("URL bar duplicate navigation guard missing %q", want)
		}
	}
}

func TestShellControlsUseCentralHandlers(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	if strings.Index(s, "window.navigateFromBar = function()") > strings.Index(s, "installShellControlHandlers();") {
		t.Fatal("navigateFromBar must be assigned before URL handlers are installed")
	}
	for _, want := range []string{
		"function installShellControlHandlers()",
		"document.getElementById('setup-action')?.addEventListener('click', saveKey)",
		"document.getElementById('url-go')?.addEventListener('click', () => window.navigateFromBar());",
		"document.getElementById('url-input')?.addEventListener('keydown', (event) => {",
		"window.navigateFromBar = function()",
		"installShellControlHandlers();",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("shell control handler wiring missing %q", want)
		}
	}
	for _, forbidden := range []string{"onclick=\"saveKey()", "onclick=\"newBlankTab()", "onclick=\"openSettings()", "onclick=\"historyBack", "onkeydown=\"if(event.key==='Enter')navigateFromBar()"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("shell controls must not use inline handler %q", forbidden)
		}
	}
}

func TestSecretInputFieldsAreBounded(t *testing.T) {
	shell := readFileForRegression(t, "frontend/index.html")
	settings := readFileForRegression(t, "frontend/settings.html")
	for _, want := range []string{
		"id=\"auth-key\" type=\"password\" placeholder=\"tskey-auth-...\" maxlength=\"4096\"",
		"id=\"unlock-password\" type=\"password\" placeholder=\"Master password\" style=\"display:none\" maxlength=\"4096\"",
	} {
		if !strings.Contains(shell, want) {
			t.Fatalf("shell secret field bound missing %q", want)
		}
	}
	for _, want := range []string{
		"id=\"pw1\" placeholder=\"Enter master password\" maxlength=\"4096\"",
		"id=\"pw2\" placeholder=\"Confirm password\" maxlength=\"4096\"",
		"id=\"ts-key\" placeholder=\"tskey-auth-...\" maxlength=\"4096\"",
	} {
		if !strings.Contains(settings, want) {
			t.Fatalf("settings secret field bound missing %q", want)
		}
	}
}

func TestRemoteDisplayPasswordFieldsAreBounded(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"data-rdp-password type=\"password\" placeholder=\"Password\" aria-label=\"RDP password\" autocomplete=\"current-password\" maxlength=\"1024\"",
		"data-vnc-password type=\"password\" placeholder=\"Password\" aria-label=\"VNC password\" autocomplete=\"current-password\" maxlength=\"8\"",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("remote display password field bound missing %q", want)
		}
	}
}

func TestRDPWebSocketReportsMalformedControlMessages(t *testing.T) {
	s := readFileForRegression(t, "rdp.go")
	for _, want := range []string{
		"ignoring malformed text control message",
		"ignoring unknown text control message",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("RDP websocket control-message guard missing %q", want)
		}
	}
}

func TestSSHWebSocketUsesPageProtocol(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"const wsProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:';",
		"new WebSocket(`${wsProtocol}//${location.host}/api/ssh/ws?${params.toString()}`)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SSH websocket protocol handling missing %q", want)
		}
	}
	if strings.Contains(s, "new WebSocket(`ws://${location.host}/api/ssh/ws?") {
		t.Fatal("SSH websocket must not hard-code ws://")
	}
}

func TestSSHWebSocketIgnoresMalformedControlMessages(t *testing.T) {
	s := readFileForRegression(t, "ws_terminal.go")
	for _, want := range []string{
		"strings.HasPrefix(trimmed, \"{\")",
		"ignoring malformed control message",
		"ignoring unknown control message",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SSH websocket control-message guard missing %q", want)
		}
	}
}

func TestSSHPromptPasswordInputIsBounded(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"const MAX_SSH_PASSWORD_CHARS = 4096;",
		"if (password.length < MAX_SSH_PASSWORD_CHARS)",
		"Authentication failed: ${err.message}",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("SSH prompt password bound missing %q", want)
		}
	}
}

func TestTerminalTitleTruncationIsUnicodeSafe(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	want := "t.title = Array.from(sanitizeBrowserTitle(title)).slice(0, 48).join('');"
	if !strings.Contains(s, want) {
		t.Fatalf("terminal title truncation must be Unicode-safe; missing %q", want)
	}
}

func TestFrontendCentralizesBrowserURLNormalization(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	if !strings.Contains(s, "function normalizeBrowserURL(url)") {
		t.Fatal("frontend missing normalizeBrowserURL")
	}
	for _, want := range []string{
		"!u.username && !u.password",
		"const browserURL = normalizeBrowserURL(text);",
		"if (browserURL) return browserURL;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend browser URL normalization missing %q", want)
		}
	}
	if strings.Contains(s, "navUrl = 'http://' + navUrl") || strings.Contains(s, "'http://' + url") {
		t.Fatal("frontend must not use ad-hoc browser URL prefixing")
	}
}

func TestRecentTabsAreDedupedByCanonicalTarget(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function recentTabKey(tab)",
		"function dedupeRecentTabs(tabs)",
		"if (!clean || !key || seen.has(key)) continue;",
		"const tabs = dedupeRecentTabs(cfg.openTabs || []);",
		"const tabs = dedupeRecentTabs(state.tabs).slice(0, 100);",
		"if (clean.type === 'vnc' || clean.type === 'rdp') return `${clean.type}:${String(clean.url || '').toLowerCase()}`;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("recent tab dedupe missing %q", want)
		}
	}
}

func TestRecentTabsLoadFailureIsVisible(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"if (!res.ok) throw new Error(await res.text());",
		"console.warn('recent tabs load failed', err);",
		"Could not load recent tabs",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("recent tabs failure handling missing %q", want)
		}
	}
}

func TestFrontendDownloadPollingHandlesStatusFailures(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"if (!res.ok) throw new Error(await res.text());",
		"clearInterval(poll);",
		"status.textContent = 'Error: ' + err.message;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend download polling error handling missing %q", want)
		}
	}
}

func TestFrontendValidatesDownloadURLs(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function downloadDisplayName(url)",
		"if (u.protocol !== 'http:' && u.protocol !== 'https:') return '';",
		"Invalid download URL",
		"let start;",
		"start = await fetch('/api/download?url=' + encodeURIComponent(url));",
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
		"if (!validTabID(fromId) || !validTabID(beforeId) || fromId === beforeId) return;",
		"validTabID(t.id)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend tab id validation missing %q", want)
		}
	}
}

func TestFrontendExpectedFailuresAreReported(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"console.warn('load URL history failed', err);",
		"console.warn('save URL history failed', err);",
		"console.warn('network indicator update failed', err);",
		"console.debug('invalid custom URL', err);",
		"console.debug('invalid URL component', err);",
		"console.debug('download display name unavailable', err);",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend failure reporting missing %q", want)
		}
	}
	for _, forbidden := range []string{"const origRenderTabs = renderTabs", "Actually just call saveOpenTabs"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("frontend should not keep stale hook comment/declaration %q", forbidden)
		}
	}
}

func TestFrontendPersistsOnlySanitizedURLState(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function normalizeHistoryURL(url)",
		"return parsed.map(normalizeHistoryURL).filter(Boolean).slice(0, 100);",
		"function sanitizeTabForSave(t)",
		"const tabs = dedupeRecentTabs(state.tabs).slice(0, 100);",
		"const clean = sanitizeTabForSave(t);",
		"return clean ? { ...clean, id: String(t.id) } : null;",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("frontend persistence sanitizer missing %q", want)
		}
	}
}

func TestNoOccludedProgressStripInShell(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, forbidden := range []string{"id=\"url-progress\"", "urlProgressIndeterminate", "#url-progress"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("legacy occluded progress strip should remain absent; found %q", forbidden)
		}
	}
	// Navigation progress now lives in the chrome-level status surface.
	if !strings.Contains(s, "function setURLProgress(active, _done = false)") {
		t.Fatal("setURLProgress should drive the chrome navigation status surface")
	}
}
