// Womprat Linux UX test: drives the real shell in Chromium against a running
// app instance with direct-dial, using a built-in RFB (VNC) stub so the VNC
// viewer can fully connect. Captures console/page errors and asserts UX flows.
import { chromium } from "playwright";
import net from "node:net";
import http from "node:http";
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { buildDir, runDir, profilePage } from './profile.mjs';
import { join } from "node:path";

const BIN = process.env.WOMPRAT_BIN || join(buildDir, 'dist/womprat-linux-debug');

function startRFBStub() {
  return new Promise((resolve) => {
    const srv = net.createServer((sock) => {
      let stage = "version";
      sock.write("RFB 003.008\n");
      sock.on("data", (buf) => {
        try {
          if (stage === "version") {
            // client version received; send 1 security type: None(1)
            sock.write(Buffer.from([1, 1]));
            stage = "security";
          } else if (stage === "security") {
            // client selected a security type (1 byte). Send SecurityResult OK.
            sock.write(Buffer.from([0, 0, 0, 0]));
            stage = "init";
          } else if (stage === "init") {
            // ClientInit (1 byte shared). Send ServerInit: 4x4, 32bpp, name.
            const name = Buffer.from("stubvnc");
            const hdr = Buffer.alloc(24);
            hdr.writeUInt16BE(4, 0); // width
            hdr.writeUInt16BE(4, 2); // height
            // pixel format (16 bytes) at offset 4
            hdr[4] = 32; // bits-per-pixel
            hdr[5] = 24; // depth
            hdr[6] = 0;  // big-endian flag
            hdr[7] = 1;  // true-colour
            hdr.writeUInt16BE(255, 8);  // red-max
            hdr.writeUInt16BE(255, 10); // green-max
            hdr.writeUInt16BE(255, 12); // blue-max
            hdr[14] = 16; // red-shift
            hdr[15] = 8;  // green-shift
            hdr[16] = 0;  // blue-shift
            hdr.writeUInt32BE(name.length, 20); // name length
            sock.write(Buffer.concat([hdr, name]));
            stage = "ready";
          }
          // ignore further client messages (SetPixelFormat/SetEncodings/etc.)
        } catch {}
      });
      sock.on("error", () => {});
    });
    srv.listen(0, "127.0.0.1", () => resolve(srv));
  });
}

function startVNCAuthStub() {
  return new Promise((resolve) => {
    const srv = net.createServer((sock) => {
      let stage = "version", pending = Buffer.alloc(0);
      const name = Buffer.from("authvnc");
      const serverInit = () => {
        const hdr = Buffer.alloc(24);
        hdr.writeUInt16BE(4, 0); hdr.writeUInt16BE(4, 2);
        hdr[4]=32; hdr[5]=24; hdr[7]=1;
        hdr.writeUInt16BE(255,8); hdr.writeUInt16BE(255,10); hdr.writeUInt16BE(255,12);
        hdr[14]=16; hdr[15]=8; hdr.writeUInt32BE(name.length,20);
        sock.write(Buffer.concat([hdr,name]));
      };
      sock.write("RFB 003.008\n");
      sock.on("data", chunk => {
        pending = Buffer.concat([pending, chunk]);
        for (;;) {
          if (stage === "version" && pending.length >= 12) {
            pending = pending.subarray(12); sock.write(Buffer.from([1,2])); stage="security";
          } else if (stage === "security" && pending.length >= 1) {
            const selected=pending[0]; pending=pending.subarray(1);
            if (selected !== 2) return sock.destroy();
            sock.write(Buffer.from([...Array(16).keys()])); stage="response";
          } else if (stage === "response" && pending.length >= 16) {
            pending=pending.subarray(16); sock.write(Buffer.from([0,0,0,0])); stage="init";
          } else if (stage === "init" && pending.length >= 1) {
            pending=pending.subarray(1); serverInit(); stage="ready";
          } else break;
        }
      });
      sock.on("error",()=>{});
    });
    srv.listen(0,"127.0.0.1",()=>resolve(srv));
  });
}

function startDownloadStub() {
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      if (req.url === "/audit.txt") {
        res.writeHead(200, { "content-type": "text/plain", "content-length": "20" });
        res.end("WOMPRAT-DOWNLOAD-OK\n");
      } else { res.writeHead(404); res.end(); }
    });
    srv.listen(0, "127.0.0.1", () => resolve(srv));
  });
}

function startWomprat(homeDir) {
  return new Promise((resolve, reject) => {
    const env = { ...process.env, HOME: homeDir, XDG_CONFIG_HOME: join(homeDir, '.config'), XDG_DATA_HOME: join(homeDir, '.local/share'), WOMPRAT_HEADLESS: "1", WOMPRAT_DIRECT: "1" };
    const p = spawn(BIN, [], { env });
    let url = null, tok = null, out = "";
    const onData = (d) => {
      out += d.toString();
      const mu = out.match(/WOMPRAT_SHELL_URL=(\S+)/);
      const mt = out.match(/WOMPRAT_TOKEN=(\S+)/);
      if (mu) url = mu[1];
      if (mt) tok = mt[1];
      if (url && tok) resolve({ proc: p, url, token: tok });
    };
    p.stdout.on("data", onData);
    p.stderr.on("data", onData);
    p.on("exit", (c) => reject(new Error("womprat exited early: " + c + "\n" + out)));
    setTimeout(() => reject(new Error("timeout waiting for womprat URL\n" + out)), 8000);
  });
}

const results = [];
function check(name, ok, detail = "") { results.push({ name, ok, detail }); console.log(`${ok ? "PASS" : "FAIL"} ${name}${detail ? " :: " + detail : ""}`); }

const rfb = await startRFBStub();
const rfbPort = rfb.address().port;
const downloadStub = await startDownloadStub();
const downloadPort = downloadStub.address().port;
const authVnc = await startVNCAuthStub();
const authVncPort = authVnc.address().port;
const testHome = mkdtempSync(join(runDir, "browser-audit-"));
const { proc, url, token } = await startWomprat(testHome);

const consoleErrors = [];
const pageErrors = [];
let browser;
let stopProfile = async () => {};
try {
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();
  stopProfile = await profilePage(page, 'ux-renderer');
  page.on("console", (m) => { if (m.type() === "error") consoleErrors.push(m.text()); });
  page.on("pageerror", (e) => pageErrors.push(String(e)));

  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("#url-input", { timeout: 5000 });
  await page.evaluate(() => document.getElementById("setup")?.classList.add("hidden"));
  check("shell loads", true);

  // The error banner must sit above native/shell panels so its buttons receive
  // pointer events. Exercise both dismiss and Settings with real mouse clicks.
  await page.evaluate(() => window.wompratContentError(null, 'DNS lookup failed for smith.local'));
  const statusSettings = page.locator('#nav-status-settings');
  const statusHit = await statusSettings.evaluate(button => {
    const rect = button.getBoundingClientRect();
    return document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2) === button;
  });
  check('error status actions receive pointer events', statusHit);
  await page.locator('#nav-status-dismiss').click();
  check('error status dismiss responds to click', await page.locator('#nav-status').isHidden());
  await page.evaluate(() => window.wompratContentError(null, 'DNS lookup failed for smith.local'));
  await statusSettings.click();
  check('error status Settings opens on click', await page.locator('#panel-settings.active iframe[src="/settings.html"]').count() === 1);

  // Native certificate consent appears in the existing banner, not a popup.
  await page.evaluate(() => {
    window.__certificateClicks = [];
    window.womprat_proceedCertificate = async (tab, token) => { window.__certificateClicks.push({tab, token}); return true; };
    window.__certificateTab = document.querySelector('#tab-list .tab.active').dataset.tabId;
    window.wompratNavigationDone(window.__certificateTab, 'https://flint.local:8443/', false, 0, 'Page failed to load.');
  });
  check('generic failure hides certificate exception', await page.locator('#nav-status-proceed').isHidden());
  await page.evaluate(() => window.wompratNavigationDone(window.__certificateTab, 'https://flint.local:8443/vault/id', false, 16, 'TLS handshake failed. (ERR_SSL_PROTOCOL_ERROR)'));
  check('concrete TLS failure stays visible', (await page.locator('#nav-status-text').textContent()).includes('ERR_SSL_PROTOCOL_ERROR'));
  const copyHit = await page.locator('#nav-status-copy').evaluate(button => { const r=button.getBoundingClientRect();return document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)===button; });
  check('copy error button receives pointer events', copyHit);
  await page.context().grantPermissions(['clipboard-read','clipboard-write']);
  await page.click('#nav-status-copy');
  check('copy includes URL and native error', await page.evaluate(async () => { const text=await navigator.clipboard.readText();return text.includes('ERR_SSL_PROTOCOL_ERROR')&&text.includes('https://flint.local:8443/vault/id'); }));
  await page.evaluate(() => window.wompratCertificatePrompt(window.__certificateTab, 'native-certificate-token', 'https://flint.local:8443/'));
  check('certificate exception button is visible', await page.locator('#nav-status-proceed').isVisible());
  check('certificate button has session warning', (await page.locator('#nav-status-proceed').getAttribute('title')).includes('browser session'));
  await page.click('#nav-status-proceed');
  check('certificate consent click forwards tab and opaque token', await page.evaluate(() => window.__certificateClicks.length === 1 && window.__certificateClicks[0].tab === window.__certificateTab && window.__certificateClicks[0].token === 'native-certificate-token'));
  check('certificate consent hides one-use button', await page.locator('#nav-status-proceed').isHidden());
  await page.evaluate(() => { window.wompratCertificatePrompt(window.__certificateTab, '', ''); window.wompratNavigationDone(window.__certificateTab, '', true); delete window.womprat_proceedCertificate; });

  // Real Chromium handshake failure, independent of the native Windows bridge:
  // HTTPS against the local plain-HTTP fixture must expose a net::ERR_ symbol.
  const failedPage = await context.newPage();
  const diagnosticCDP = await context.newCDPSession(failedPage);
  const tree = await diagnosticCDP.send('Page.getFrameTree');
  const mainFrame = tree.frameTree.frame.id;
  const failedRequests = new Set();
  let handshakeError = '';
  diagnosticCDP.on('Network.requestWillBeSent', event => { if (event.type === 'Document' && event.frameId === mainFrame) failedRequests.add(event.requestId); });
  diagnosticCDP.on('Network.loadingFailed', event => { if (failedRequests.has(event.requestId)) handshakeError = event.errorText; });
  await diagnosticCDP.send('Network.enable');
  await failedPage.goto(`https://127.0.0.1:${downloadPort}/`, { timeout: 5000 }).catch(() => {});
  check('Chromium reports real TLS handshake failure', handshakeError.includes('ERR_SSL_PROTOCOL_ERROR'), handshakeError);
  await diagnosticCDP.detach();
  await failedPage.close();

  // Routed download manager: start, poll to completion, and verify file bytes.
  await page.evaluate((u) => window.triggerDownload(u), `http://127.0.0.1:${downloadPort}/audit.txt`);
  const downloadComplete = await page.waitForFunction(() => document.getElementById("dl-status")?.textContent === "Saved to Downloads", { timeout: 10000 }).then(() => true).catch(() => false);
  const downloadedPath = join(testHome, "Downloads", "audit.txt");
  const downloadStatus = await page.locator("#dl-status").textContent().catch(() => "missing");
  const downloadedBytes = existsSync(downloadedPath) ? readFileSync(downloadedPath, "utf8") : "";
  check("download completes through managed route", downloadComplete && downloadedBytes === "WOMPRAT-DOWNLOAD-OK\n", `${downloadStatus}; path=${downloadedPath}; bytes=${JSON.stringify(downloadedBytes)}`);

  // 1) Settings opens and renders.
  await page.evaluate(() => window.openSettings && window.openSettings());
  const hasSettings = await page.waitForSelector("#panel-settings iframe, iframe.browser-frame", { timeout: 5000 }).then(() => true).catch(() => false);
  check("settings panel renders", hasSettings);
  const settingsFrame = page.frameLocator('#panel-settings iframe[src="/settings.html"]');
  const dnsFormGeometry = async () => settingsFrame.locator('#diag-dns-form').evaluate(form => {
    const rect = selector => form.querySelector(selector).getBoundingClientRect();
    const outer = form.getBoundingClientRect();
    const label = rect('label'), input = rect('input'), lookup = rect('#diag-dns-run'), copy = rect('#diag-dns-copy');
    const within = r => r.left >= outer.left - 1 && r.right <= outer.right + 1;
    return { labelAbove: label.bottom <= input.top + 1, controlsAligned: Math.abs(lookup.top - copy.top) <= 2,
      noOverlap: input.right <= lookup.left + 1 || input.bottom <= lookup.top + 1,
      contained: [label, input, lookup, copy].every(within) };
  });
  await page.setViewportSize({width: 894, height: 600});
  const desktopDNS = await dnsFormGeometry();
  if (process.env.WOMPRAT_UX_SCREENSHOTS) await settingsFrame.locator('#diag-dns-form').screenshot({path:join(process.env.WOMPRAT_UX_SCREENSHOTS, 'dns-form-wide.png')});
  check('DNS form aligns label and controls at screenshot width', desktopDNS.labelAbove && desktopDNS.controlsAligned && desktopDNS.noOverlap && desktopDNS.contained, JSON.stringify(desktopDNS));
  await page.setViewportSize({width: 540, height: 600});
  const narrowDNS = await dnsFormGeometry();
  if (process.env.WOMPRAT_UX_SCREENSHOTS) await settingsFrame.locator('#diag-dns-form').screenshot({path:join(process.env.WOMPRAT_UX_SCREENSHOTS, 'dns-form-narrow.png')});
  check('DNS form aligns and contains controls when narrow', narrowDNS.labelAbove && narrowDNS.controlsAligned && narrowDNS.noOverlap && narrowDNS.contained, JSON.stringify(narrowDNS));
  await page.setViewportSize({width: 1280, height: 720});
  await settingsFrame.locator('#diag-dns-name').fill('smith.local');
  await settingsFrame.locator('#diag-dns-run').click();
  await settingsFrame.locator('#diag-dns-results .diag-card').first().waitFor({ timeout: 5000 });
  const smithEvidence = await settingsFrame.locator('#diag-dns-results').textContent();
  const smithJSON = await settingsFrame.locator('#diag-dns-output').textContent();
  check('generic DNS form displays disconnected app state and copyable evidence', smithEvidence.includes('Tailscale path') && smithEvidence.includes('disconnected') && smithJSON.includes('"name": "smith.local"') && smithJSON.includes('"queries": []'));
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await settingsFrame.locator('#diag-dns-copy').click();
  await settingsFrame.locator('#diag-dns-status').getByText('DNS results copied to clipboard.').waitFor();
  const copiedDNS = await page.evaluate(() => navigator.clipboard.readText());
  check('DNS results copy as readable report', copiedDNS.includes('DNS lookup: smith.local') && copiedDNS.includes('Tailscale: disconnected') && copiedDNS.includes('Womprat tsnet is not connected'));

  // Search settings and address-bar classification need no live internet.
  await settingsFrame.locator('button[data-tab="browser"]').click();
  const providerSelect = settingsFrame.locator('#search-provider');
  check('address search defaults to DuckDuckGo', await providerSelect.inputValue() === 'duckduckgo');
  await providerSelect.selectOption('google');
  await settingsFrame.locator('#search-provider-status').filter({hasText:'Search engine saved.'}).waitFor();
  check('search provider saves in browser settings', await page.evaluate(async () => (await (await fetch('/api/settings/browser/search')).json()).searchProvider === 'google'));
  await page.evaluate(() => { window.newBlankTab(); window.__searchTab=document.querySelector('#tab-list .tab.active').dataset.tabId; window.__searchTargets=[]; window.womprat_setChromeHeight=px=>window.__historyChromeHeight=px; window.womprat_navigate=async target => window.__searchTargets.push(target); });
  await page.locator('#url-input').fill('café & C++ #guide');
  await page.locator('#url-input').press('Enter');
  await page.waitForFunction(() => window.__searchTargets?.length === 1);
  check('Enter searches with current provider and encoded query', await page.evaluate(() => {const u=new URL(window.__searchTargets[0]);return u.hostname==='www.google.com'&&u.searchParams.get('q')==='café & C++ #guide';}));
  await page.locator('#url-input').fill('https://flint.local:8443/vault/id');
  await page.locator('#url-input').press('Enter');
  await page.waitForFunction(() => window.__searchTargets?.length === 2);
  check('explicit HTTPS URL bypasses search', await page.evaluate(() => window.__searchTargets[1]==='https://flint.local:8443/vault/id'));
  await page.evaluate(async () => {await fetch('/api/settings/browser/search',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({searchProvider:'duckduckgo'})});});
  await page.locator('#url-input').fill('weather tomorrow');
  await page.locator('#url-input').press('Enter');
  await page.waitForFunction(() => window.__searchTargets?.length === 3);
  check('DuckDuckGo is used immediately after changing provider', await page.evaluate(() => new URL(window.__searchTargets[2]).hostname==='duckduckgo.com'));
  await page.locator('#url-input').fill('weather');
  check('history suggests previous search text', await page.locator('#address-suggestions button').filter({hasText:'weather tomorrow'}).isVisible());
  await page.locator('#url-input').press('ArrowDown');
  await page.locator('#url-input').press('Enter');
  await page.waitForFunction(() => window.__searchTargets?.length === 4);
  check('keyboard accepts history search once', await page.evaluate(() => new URL(window.__searchTargets[3]).searchParams.get('q')==='weather tomorrow'));
  await page.locator('#url-input').fill('flint');
  const historyURL=page.locator('#address-suggestions button').filter({hasText:'https://flint.local:8443/vault/id'});
  check('history matches hostname without scheme', await historyURL.isVisible());
  check('history suggestions stay inside reported shell chrome', await page.locator('#address-suggestions').evaluate(el => el.getBoundingClientRect().bottom * devicePixelRatio <= window.__historyChromeHeight + 1));
  await historyURL.click();
  await page.waitForFunction(() => window.__searchTargets?.length === 5);
  check('mouse accepts history URL without searching', await page.evaluate(() => window.__searchTargets[4]==='https://flint.local:8443/vault/id'));
  await page.locator('#url-input').fill('weather');
  await page.locator('#url-input').press('Escape');
  check('Escape hides history without navigating', await page.locator('#address-suggestions').isHidden() && await page.evaluate(() => window.__searchTargets.length===5));
  await page.locator('#url-input').fill('no-match-history-xyz');
  check('unmatched input hides suggestions', await page.locator('#address-suggestions').isHidden());
  await page.locator('#url-input').fill('weather');
  await page.locator('#url-input').press('Tab');
  check('leaving address field dismisses history', await page.locator('#address-suggestions').isHidden());
  await page.evaluate(() => {delete window.womprat_navigate;delete window.womprat_setChromeHeight;window.closeTab(window.__searchTab);window.openSettings();});

  // 2) VNC URL connects end-to-end via RFB stub.
  await page.evaluate(() => { window.newBlankTab && window.newBlankTab(); });
  await page.fill("#url-input", `vnc://127.0.0.1:${rfbPort}`);
  await page.press("#url-input", "Enter");
  const vncPanel = await page.waitForSelector(".vnc-panel", { timeout: 5000 }).then(() => true).catch(() => false);
  check("vnc panel created", vncPanel);
  let vncStatus = "";
  const vncConnected = await page.waitForFunction(() => {
    const el = document.querySelector("[data-vnc-status]");
    const t = el ? el.textContent : "";
    return /4×4|stubvnc|RFB|Authenticated|Initializing|×/.test(t);
  }, { timeout: 8000 }).then(() => true).catch(() => false);
  vncStatus = await page.evaluate(() => (document.querySelector("[data-vnc-status]")||{}).textContent || "");
  check("vnc connects (status)", vncConnected, vncStatus);
  check("vnc replaces blank tab", await page.evaluate(() => !Array.from(document.querySelectorAll('.tab-title')).some(el => el.textContent === 'New tab')));

  // Password-authenticated VNC: first connection prompts, reconnect completes.
  await page.evaluate(() => window.newBlankTab());
  await page.fill("#url-input", `vnc://127.0.0.1:${authVncPort}`);
  await page.press("#url-input", "Enter");
  await page.waitForSelector('.vnc-panel[data-vnc-auth-required] .vnc-dialog', {timeout:5000});
  check("vnc password dialog appears", await page.locator('.vnc-panel[data-vnc-auth-required] .vnc-dialog').isVisible());
  await page.fill('.vnc-panel[data-vnc-auth-required] [data-vnc-password]', 'secret');
  await page.click('.vnc-panel[data-vnc-auth-required] [data-vnc-connect]');
  const authConnected = await page.waitForFunction(() => document.querySelector('.panel.active .vnc-panel[data-connected] [data-vnc-status]')?.textContent?.includes('authvnc'), {timeout:8000}).then(()=>true).catch(()=>false);
  check("vnc password reconnect authenticates", authConnected);

  // 3) RDP URL routes to an RDP panel (no full server; expect graceful status).
  await page.evaluate(() => { window.newBlankTab && window.newBlankTab(); });
  await page.fill("#url-input", "rdp://me@127.0.0.1:3389");
  await page.press("#url-input", "Enter");
  const rdpPanel = await page.waitForSelector(".rdp-panel", { timeout: 5000 }).then(() => true).catch(() => false);
  check("rdp panel created", rdpPanel);
  const rdpHostShown = await page.evaluate(() => {
    const tabs = Array.from(document.querySelectorAll(".tab-title")).map(t => t.textContent);
    return JSON.stringify(tabs);
  });

  // 4) ssh URL routes to a terminal tab.
  await page.evaluate(() => { window.newBlankTab && window.newBlankTab(); });
  await page.fill("#url-input", "ssh://me@127.0.0.1:22");
  await page.press("#url-input", "Enter");
  const termPanel = await page.waitForSelector(".term-panel, .term-container", { timeout: 5000 }).then(() => true).catch(() => false);
  check("ssh terminal panel created", termPanel);
  const terminalFontReady = await page.waitForFunction(() => {
    const terminal = document.querySelector('.term-panel.active .xterm');
    return !!terminal && document.fonts.check('400 14px "FiraCode Nerd Font Mono"') && document.fonts.check('700 14px "FiraCode Nerd Font Mono"');
  }, {timeout: 5000}).then(() => true).catch(() => false);
  check("terminal font faces loaded before rendering", terminalFontReady);
  const recentTerminal = await page.waitForFunction(() =>
    document.getElementById('recent-list')?.textContent?.includes('127.0.0.1'), {timeout: 5000}).then(() => true).catch(() => false);
  check("recent tabs refresh after save", recentTerminal);
  const recentGrid = await page.evaluate(() => {
    const list = document.getElementById('recent-list');
    return { count: list.querySelectorAll('.recent-item').length, layout: getComputedStyle(list).display, columns: getComputedStyle(list).gridTemplateColumns.split(' ').length };
  });
  check('home recent items use responsive grid', recentGrid.count > 0 && recentGrid.count <= 10 && recentGrid.layout === 'grid' && recentGrid.columns >= 1);
  const recentBeforeRemoval = await page.locator('#recent-list .recent-item').count();
  await page.evaluate(() => { window.newBlankTab && window.newBlankTab(); });
  const removalTab = await page.locator('#tab-list .tab.active').getAttribute('data-tab-id');
  const activeTabsBeforeRemove = await page.locator('#tab-list .tab').count();
  await page.locator('#recent-list .recent-remove').first().click();
  await page.locator('#recent-status').getByText('Removed from Recent. Open tabs are unchanged.').waitFor();
  const recentAfterRemoval = await page.locator('#recent-list .recent-item').count();
  const hiddenRecentSaved = await page.evaluate(async () => (await (await fetch('/api/settings/config')).json()).hiddenRecent.length);
  check('Recent remove hides card without opening or closing tabs', recentAfterRemoval === recentBeforeRemoval - 1 && hiddenRecentSaved === 1 && await page.locator('.tab').count() === activeTabsBeforeRemove);
  if (removalTab) await page.evaluate(id => window.closeTab(id), removalTab);
  const freshPage = await context.newPage();
  await freshPage.goto(url, {waitUntil:'domcontentloaded'});
  const hiddenInFreshPage = await freshPage.evaluate(async () => (await (await fetch('/api/settings/config')).json()).hiddenRecent.length);
  const savedInput = await freshPage.evaluate(async () => (await (await fetch('/api/settings/browser/history')).json()).history.includes('weather tomorrow'));
  await freshPage.evaluate(() => document.getElementById('setup')?.classList.add('hidden'));
  await freshPage.evaluate(() => {window.newBlankTab();});
  await freshPage.locator('#url-input').fill('weather');
  await freshPage.locator('#address-suggestions button').filter({hasText:'weather tomorrow'}).waitFor();
  check('saved history is available in a fresh shell', savedInput && await freshPage.locator('#address-suggestions button').filter({hasText:'weather tomorrow'}).isVisible());
  await freshPage.close();
  check('Recent removal persists in a fresh page', hiddenInFreshPage === hiddenRecentSaved);
  check("custom schemes leave no blank placeholders", await page.evaluate(() => !Array.from(document.querySelectorAll('.tab-title')).some(el => el.textContent === 'New tab')));

  // Updating title metadata must retain the tab element (and therefore avoid
  // tearing down drag/focus state or flickering the whole strip).
  const titleUpdateStable = await page.evaluate(() => {
    const tab = document.querySelector('#tab-list .tab');
    if (!tab) return false;
    const id = tab.dataset.tabId;
    window.wompratSetTabMeta(id, 'Updated title 🐀', '');
    return document.querySelector(`#tab-list .tab[data-tab-id="${CSS.escape(id)}"]`) === tab && tab.querySelector('.tab-title')?.textContent === 'Updated title 🐀';
  });
  check("tab metadata updates in place", titleUpdateStable);
  const loadingStable = await page.evaluate(() => {
    const tabs=Array.from(document.querySelectorAll('#tab-list .tab'));
    const icons=tabs.map(tab=>tab.querySelector('.tab-icon-slot'));
    const target=tabs.at(-1), id=target.dataset.tabId;
    // showBrowserTab and completion exercise loading plus lifecycle render calls.
    window.showBrowserTab(id,'https://flint.local:8443/','loading',{skipNative:true});
    const started=Array.from(document.querySelectorAll('#tab-list .tab'));
    const sameStart=started.length===tabs.length&&started.every((tab,i)=>tab===tabs[i]&&(tab===target||tab.querySelector('.tab-icon-slot')===icons[i]));
    window.wompratNavigationDone(id,'https://flint.local:8443/',true);
    const ended=Array.from(document.querySelectorAll('#tab-list .tab'));
    return sameStart&&ended.length===tabs.length&&ended.every((tab,i)=>tab===tabs[i]&&(tab===target||tab.querySelector('.tab-icon-slot')===icons[i]));
  });
  check('loading and navigation preserve every tab and inactive icon', loadingStable);

  // Standard tab lifecycle over the real tabs created above: reorder by stable
  // id through drag-and-drop, then close that exact tab.
  const tabOrder = await page.evaluate(() => Array.from(document.querySelectorAll("#tab-list .tab")).map(t => t.dataset.tabId).filter(Boolean));
  const moving = tabOrder.at(-1), before = tabOrder.at(-2);
  await page.locator(`#tab-list .tab[data-tab-id="${moving}"]`).dragTo(page.locator(`#tab-list .tab[data-tab-id="${before}"]`));
  const reordered = await page.evaluate(() => Array.from(document.querySelectorAll("#tab-list .tab")).map(t => t.dataset.tabId).filter(Boolean));
  check("tab reorder uses stable ids", reordered.indexOf(moving) + 1 === reordered.indexOf(before), JSON.stringify(reordered));
  await page.locator(`#tab-list .tab[data-tab-id="${moving}"] .tab-close`).click();
  await page.waitForFunction((id) => !document.querySelector(`#tab-list .tab[data-tab-id="${id}"]`), moving);
  check("tab close removes exact tab", await page.locator(`#tab-list .tab[data-tab-id="${moving}"]`).count() === 0);

  check("no pageerror", pageErrors.length === 0, pageErrors.join(" | "));
  check("no console errors", consoleErrors.length === 0, consoleErrors.slice(0,5).join(" | "));
  console.log("RDP tabs:", rdpHostShown);
} finally {
  try { await stopProfile(); } finally {
    if (browser) await browser.close();
    if (proc.exitCode === null && proc.signalCode === null) {
      const exited = new Promise(resolve => proc.once('exit', resolve));
      proc.kill("SIGTERM");
      await exited;
    }
    rfb.close();
    downloadStub.close();
    authVnc.close();
    rmSync(testHome, { recursive: true, force: true });
  }
}

const failed = results.filter(r => !r.ok);
console.log(`\n=== ${results.length - failed.length}/${results.length} checks passed ===`);
process.exitCode = failed.length ? 1 : 0;
