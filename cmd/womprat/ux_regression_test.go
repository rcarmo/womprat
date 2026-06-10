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
	if strings.Contains(s, "catch {}") {
		t.Fatal("settings must not contain empty catch blocks")
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
		"setStatus('keys-status', 'error', 'Invalid key name')",
		"setStatus('keys-status', 'ok', 'Key imported')",
		"setStatus('keys-status', 'ok', 'Key generated')",
		"setStatus('hosts-status', 'error', 'Invalid host')",
		"setStatus('hosts-status', 'ok', 'Host updated')",
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

func TestSettingsActivationIsResilient(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"window.openSettings = function()",
		"if (window.womprat_openSettings) { try { womprat_openSettings(); } catch (err) { console.warn('native settings open failed', err); } }",
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

func TestSetupAuthReportsFailures(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"Auth status unavailable:",
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

func TestShellControlsUseCentralHandlers(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function installShellControlHandlers()",
		"document.getElementById('setup-action')?.addEventListener('click', saveKey)",
		"document.getElementById('url-input')?.addEventListener('keydown'",
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

func TestSSHPromptPasswordInputIsBounded(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"const MAX_SSH_PASSWORD_CHARS = 4096;",
		"if (password.length < MAX_SSH_PASSWORD_CHARS)",
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

func TestFrontendPersistsOnlySanitizedURLState(t *testing.T) {
	s := readFileForRegression(t, "frontend/index.html")
	for _, want := range []string{
		"function normalizeHistoryURL(url)",
		"return parsed.map(normalizeHistoryURL).filter(Boolean).slice(0, 100);",
		"function sanitizeTabForSave(t)",
		"const tabs = state.tabs.map(sanitizeTabForSave).filter(Boolean).slice(0, 100);",
		"const clean = sanitizeTabForSave(t);",
		"return clean ? { ...clean, id: String(t.id) } : null;",
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
