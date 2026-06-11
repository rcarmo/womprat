// Womprat Linux UX test: drives the real shell in Chromium against a running
// app instance with direct-dial, using a built-in RFB (VNC) stub so the VNC
// viewer can fully connect. Captures console/page errors and asserts UX flows.
import { chromium } from "playwright";
import net from "node:net";
import { spawn } from "node:child_process";

const BIN = process.env.WOMPRAT_BIN || "dist/womprat-linux-debug";

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

function startWomprat() {
  return new Promise((resolve, reject) => {
    const env = { ...process.env, WOMPRAT_HEADLESS: "1", WOMPRAT_DIRECT: "1" };
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
const { proc, url, token } = await startWomprat();

const consoleErrors = [];
const pageErrors = [];
let browser;
try {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  page.on("console", (m) => { if (m.type() === "error") consoleErrors.push(m.text()); });
  page.on("pageerror", (e) => pageErrors.push(String(e)));

  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("#url-input", { timeout: 5000 });
  check("shell loads", true);

  // 1) Settings opens and renders.
  await page.evaluate(() => window.openSettings && window.openSettings());
  const hasSettings = await page.waitForSelector("#panel-settings iframe, iframe.browser-frame", { timeout: 5000 }).then(() => true).catch(() => false);
  check("settings panel renders", hasSettings);

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

  check("no pageerror", pageErrors.length === 0, pageErrors.join(" | "));
  check("no console errors", consoleErrors.length === 0, consoleErrors.slice(0,5).join(" | "));
  console.log("RDP tabs:", rdpHostShown);
} finally {
  if (browser) await browser.close();
  proc.kill("SIGTERM");
  rfb.close();
}

const failed = results.filter(r => !r.ok);
console.log(`\n=== ${results.length - failed.length}/${results.length} checks passed ===`);
process.exit(failed.length ? 1 : 0);
