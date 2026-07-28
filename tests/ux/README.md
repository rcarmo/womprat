# Linux UX tests (Playwright)

These tests drive the real Womprat shell in headless Chromium against a Linux
headless app instance. They are intended to catch shell/runtime regressions that
plain Go tests cannot see.

Two harnesses are checked in:

- `ux.mjs` — fast shell smoke test. It uses the direct-dial debug bypass and a
  built-in RFB stub for a lightweight VNC route check.
- `real-remotes.mjs` — integration proof against real VNC/RDP servers. It
  verifies browser canvas pixels, not only connection status text.

Both harnesses require a debug build because `WOMPRAT_DIRECT=1` is deliberately
ignored by release builds.

## Build the Linux debug binary

```bash
go build -ldflags='-X main.debugBuild=1' -o dist/womprat-linux-debug ./cmd/womprat
```

Set `WOMPRAT_BIN` to override the binary path.

## Install Playwright + Chromium

From the repository root or this directory:

```bash
cd tests/ux
bun add -d playwright
bun x playwright install chromium
```

If browsers are installed under the default user cache, run with:

```bash
PLAYWRIGHT_BROWSERS_PATH=$HOME/.cache/ms-playwright bun run ux.mjs
```

## Fast shell smoke

```bash
cd tests/ux
PLAYWRIGHT_BROWSERS_PATH=$HOME/.cache/ms-playwright bun run ux.mjs
```

This exercises the app shell, URL routing, settings panel, VNC panel creation,
RDP panel creation, SSH terminal panel creation, stable-ID tab reorder/close, and
a managed HTTP download whose saved bytes are checked on disk. It also fails on
page or console errors.

## Real remote pixel proof

`real-remotes.mjs` expects real servers. Defaults:

```text
VNC_TARGET=vnc://127.0.0.1:5902
RDP_TARGET=rdp://127.0.0.1:3389
RDP_USER=womptest
RDP_PASS=womptest
```

Run both:

```bash
cd tests/ux
PLAYWRIGHT_BROWSERS_PATH=$HOME/.cache/ms-playwright \
  RDP_USER=womptest RDP_PASS=womptest \
  bun run real-remotes.mjs
```

Run only RDP:

```bash
WOMPRAT_UX_SKIP_VNC=1 RDP_USER=womptest RDP_PASS=womptest bun run tests/ux/real-remotes.mjs
```

Run only VNC:

```bash
WOMPRAT_UX_SKIP_RDP=1 VNC_TARGET=vnc://127.0.0.1:5902 bun run tests/ux/real-remotes.mjs
```

The RDP harness uses the current credential-dialog UX: it opens the RDP tab,
fills `.rdp-dialog [data-rdp-user]` and `.rdp-dialog [data-rdp-password]`, clicks
Connect, waits for the canvas, and then samples framebuffer pixels. After
resizing the browser it verifies that the WebSocket remains unchanged, the
credential dialog stays hidden, the canvas covers the complete viewport, and
the visual centre maps to the centre of the remote framebuffer. When the server
advertises Display Control, it also waits for the backing canvas dimensions to
match the resized content viewport. The active title and negotiated codecs are
recorded in the log and a screenshot is written to
`dist/ux-artifacts/rdp-browser-proof.png`.

The VNC harness waits for a real server-reported size in `[data-vnc-status]`,
then samples `.vnc-panel canvas` for non-uniform pixels. Current VNC negotiation
is Raw-first for correctness, with Hextile/CopyRect/ZRLE/RRE/CoRRE still
advertised afterward. Its screenshot is written to
`dist/ux-artifacts/vnc-browser-proof.png`.

## Useful local real-server setup

Example VNC test server:

```bash
Xvfb :78 -screen 0 1024x768x24 &
DISPLAY=:78 openbox &
DISPLAY=:78 xterm -geometry 80x20+120+120 -fa Monospace -fs 18 \
  -e sh -c 'echo WOMPRAT-VNC-LINUX; while true; do date; sleep 2; done' &
x11vnc -display :78 -rfbport 5902 -localhost -forever -shared -nopw -quiet &
```

Example RDP test target uses local `xrdp` on `127.0.0.1:3389` with a test user.
The previous integration runs used:

```text
user: womptest
pass: womptest
```

## Test boundaries

The browser-level harnesses run the Linux shell in headless Chromium. They cover
the shared frontend and bridge protocols, including real framebuffer output,
but they do not instantiate the native Windows WebView2 child-window host.
Windows-specific behaviour is covered by Go regression tests plus ARM64/x64
cross-build and vet checks; final release validation should still include a
manual run on Windows.

## Exit codes

Both harnesses exit non-zero if any assertion fails, if a browser page error is
raised, or if console errors are observed.
