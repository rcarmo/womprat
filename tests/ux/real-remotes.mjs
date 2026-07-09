// Real remote UX proof for Womprat.
// Requires real VNC/RDP servers and verifies browser canvas pixels, not just
// connection status. Intended for local/integration runs, not stub-only CI.
import { chromium } from "playwright";
import { spawn } from "node:child_process";
import { mkdirSync } from "node:fs";

const ARTIFACT_DIR = process.env.WOMPRAT_UX_ARTIFACT_DIR || "dist/ux-artifacts";
mkdirSync(ARTIFACT_DIR, { recursive: true });

const BIN = process.env.WOMPRAT_BIN || "dist/womprat-linux-debug";
const VNC_TARGET = process.env.VNC_TARGET || "vnc://127.0.0.1:5902";
const RDP_TARGET = process.env.RDP_TARGET || "rdp://127.0.0.1:3389";
const RDP_USER = process.env.RDP_USER || "womptest";
const RDP_PASS = process.env.RDP_PASS || "womptest";
const RUN_VNC = process.env.WOMPRAT_UX_SKIP_VNC !== "1";
const RUN_RDP = process.env.WOMPRAT_UX_SKIP_RDP !== "1";

function startWomprat() {
  return new Promise((resolve, reject) => {
    const env = { ...process.env, WOMPRAT_HEADLESS: "1", WOMPRAT_DIRECT: "1" };
    const p = spawn(BIN, [], { env });
    let url = null, token = null, out = "";
    let settled = false;
    const onData = (d) => {
      const text = d.toString();
      process.stdout.write("[womprat] " + text);
      out += text;
      const mu = out.match(/WOMPRAT_SHELL_URL=(\S+)/);
      const mt = out.match(/WOMPRAT_TOKEN=(\S+)/);
      if (mu) url = mu[1];
      if (mt) token = mt[1];
      if (!settled && url && token) {
        settled = true;
        resolve({ proc: p, url });
      }
    };
    p.stdout.on("data", onData);
    p.stderr.on("data", onData);
    p.on("exit", (c) => {
      if (!settled) reject(new Error("womprat exited early: " + c + "\n" + out));
    });
    setTimeout(() => {
      if (!settled) reject(new Error("timeout waiting for womprat URL\n" + out));
    }, 8000);
  });
}

async function sampleCanvas(page, selector) {
  return await page.evaluate((sel) => {
    const c = document.querySelector(sel);
    if (!c || !c.width || !c.height) return { ok: false, reason: "no canvas size", w: c?.width, h: c?.height };
    const ctx = c.getContext("2d");
    let img;
    try { img = ctx.getImageData(0, 0, c.width, c.height); }
    catch (e) { return { ok: false, reason: "getImageData failed: " + e, w: c.width, h: c.height }; }
    const d = img.data;
    const first = [d[0], d[1], d[2]];
    let distinct = 0, nonzero = 0;
    for (let i = 0; i < d.length; i += 4) {
      if (d[i] || d[i + 1] || d[i + 2]) nonzero++;
      if (d[i] !== first[0] || d[i + 1] !== first[1] || d[i + 2] !== first[2]) distinct++;
    }
    return { ok: distinct > 50 && nonzero > 0, w: c.width, h: c.height, distinct, nonzero };
  }, selector);
}

async function waitForPixels(page, selector, attempts, delayMs) {
  let px = { ok: false };
  for (let i = 0; i < attempts; i++) {
    await page.waitForTimeout(delayMs);
    px = await sampleCanvas(page, selector);
    if (px.ok) break;
  }
  return px;
}

async function openBlank(page) {
  await page.evaluate(() => { window.newBlankTab && window.newBlankTab(); });
}

async function testVNC(page) {
  await openBlank(page);
  await page.fill("#url-input", VNC_TARGET);
  await page.press("#url-input", "Enter");
  await page.waitForSelector(".vnc-panel canvas", { timeout: 8000 });
  await page.waitForFunction(() => /\d+\s*[×x]\s*\d+/.test(document.querySelector("[data-vnc-status]")?.textContent || ""), { timeout: 20000 });
  const px = await waitForPixels(page, ".vnc-panel canvas", 30, 500);
  const status = await page.evaluate(() => document.querySelector("[data-vnc-status]")?.textContent || "");
  const diag = await page.evaluate(() => {
    const root = document.querySelector(".vnc-panel");
    const v = root && root.__wompratVncViewer;
    const c = document.querySelector(".vnc-panel canvas");
    return { hasViewer: !!v, fbw: v?.framebufferWidth, fbh: v?.framebufferHeight, canvasW: c?.width, canvasH: c?.height, wsState: v?.ws?.readyState };
  });
  await page.screenshot({ path: `${ARTIFACT_DIR}/vnc-browser-proof.png`, fullPage: true });
  console.log("VNC status:", JSON.stringify(status));
  console.log("VNC canvas:", JSON.stringify(px));
  console.log("VNC diag:", JSON.stringify(diag));
  return /\d+\s*[×x]\s*\d+/.test(status) && px.ok;
}

async function testRDP(page) {
  await openBlank(page);
  await page.fill("#url-input", RDP_TARGET);
  await page.press("#url-input", "Enter");
  await page.waitForSelector(".rdp-dialog [data-rdp-user]", { timeout: 10000 });
  await page.fill(".rdp-dialog [data-rdp-user]", RDP_USER);
  await page.fill(".rdp-dialog [data-rdp-password]", RDP_PASS);
  await page.evaluate(() => {
    const depth = document.querySelector("[data-rdp-depth]"); if (depth) depth.value = "24";
    const nla = document.querySelector("[data-rdp-nla]"); if (nla) nla.checked = false;
    const audio = document.querySelector("[data-rdp-audio]"); if (audio) audio.checked = false;
    document.querySelector("[data-rdp-connect]")?.click();
  });
  await page.waitForSelector(".rdp-viewport canvas", { timeout: 10000 });
  let px = { ok: false };
  for (let i = 0; i < 60; i++) {
    await page.waitForTimeout(1000);
    px = await sampleCanvas(page, ".rdp-panel canvas");
    if (px.ok) break;
    const status = await page.evaluate(() => document.querySelector("[data-rdp-status]")?.textContent || "");
    if (/failed|error|denied|closed/i.test(status)) break;
  }
  const status = await page.evaluate(() => document.querySelector("[data-rdp-status]")?.textContent || "");
  const caps = await page.evaluate(() => document.querySelector("[data-rdp-caps]")?.textContent || "");
  const beforeSocket = await page.evaluateHandle(() => document.querySelector(".rdp-panel")?.__wompratRdp?.client?.socket);
  await page.setViewportSize({ width: 1100, height: 760 });
  await page.waitForTimeout(1200);
  const diag = await page.evaluate((socket) => {
    const root = document.querySelector(".rdp-panel");
    const r = root && root.__wompratRdp;
    const c = document.querySelector(".rdp-panel canvas");
    const vp = document.querySelector(".rdp-viewport");
    const dialog = document.querySelector(".rdp-dialog");
    const title = document.querySelector(".tab.active .tab-title")?.textContent || "";
    const cr = c?.getBoundingClientRect(), vr = vp?.getBoundingClientRect();
    const coverage = cr && vr && vr.width && vr.height ? (cr.width * cr.height) / (vr.width * vr.height) : 0;
    return {
      hasViewer: !!r, canvasW: c?.width, canvasH: c?.height,
      wsState: r?.client?.socket?.readyState, socketStable: r?.client?.socket === socket,
      authDialogHidden: dialog ? getComputedStyle(dialog).display === "none" : false,
      coverage, title
    };
  }, beforeSocket);
  await beforeSocket.dispose();
  await page.screenshot({ path: `${ARTIFACT_DIR}/rdp-browser-proof.png`, fullPage: true });
  console.log("RDP status:", JSON.stringify(status));
  console.log("RDP caps:", JSON.stringify(caps));
  console.log("RDP canvas:", JSON.stringify(px));
  console.log("RDP diag:", JSON.stringify(diag));
  const capsReady = caps && !/pending/i.test(caps) && /RemoteFX|NSCodec|bitmap/i.test(caps);
  return px.ok && /^Connected/.test(status) && capsReady && diag.socketStable && diag.authDialogHidden && diag.coverage > 0.75;
}

const consoleErrors = [];
const pageErrors = [];
const results = [];
const { proc, url } = await startWomprat();
let browser;
try {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  page.on("console", (m) => {
    if (m.type() === "error") consoleErrors.push(m.text());
    console.log("[console." + m.type() + "]", m.text());
  });
  page.on("pageerror", (e) => {
    pageErrors.push(e.stack || String(e));
    console.log("[pageerror]", e.stack || String(e));
  });
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("#url-input", { timeout: 5000 });
  // The integration harness deliberately uses WOMPRAT_DIRECT and no stored
  // Tailscale key. Hide the setup gate so screenshots prove the actual remote
  // framebuffer rather than an auth overlay; this does not alter connection code.
  await page.evaluate(() => document.getElementById("setup")?.classList.add("hidden"));

  if (RUN_VNC) results.push(["vnc renders real framebuffer", await testVNC(page)]);
  if (RUN_RDP) results.push(["rdp renders real framebuffer", await testRDP(page)]);
  results.push(["no pageerror", pageErrors.length === 0]);
  results.push(["no console errors", consoleErrors.length === 0]);
} finally {
  if (browser) await browser.close();
  proc.kill("SIGTERM");
}

for (const [name, ok] of results) console.log(`${ok ? "PASS" : "FAIL"} ${name}`);
const failed = results.filter(([, ok]) => !ok);
console.log(`\n=== ${results.length - failed.length}/${results.length} checks passed ===`);
process.exit(failed.length ? 1 : 0);
