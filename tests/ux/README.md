# Browser and network tests

The Playwright tests run Womprat's Linux headless shell in Chromium. They cover frontend layout, interaction and protocol bridges. They do not instantiate the native Windows WebView2 host.

## Set up

From the repository root:

```bash
bun add --cwd tests/ux -d playwright
cd tests/ux
bun x playwright install chromium
cd ../..
make ux-test
```

`make ux-test` builds `dist/womprat-linux-debug`, runs `tests/ux/ux.mjs`, and runs the dynamic-title reporter test. It uses the browser cache at `$HOME/.cache/ms-playwright`.

To build and run separately:

```bash
go build -ldflags='-X main.debugBuild=1' -o dist/womprat-linux-debug ./cmd/womprat
cd tests/ux
PLAYWRIGHT_BROWSERS_PATH=$HOME/.cache/ms-playwright bun run ux.mjs
```

Set `WOMPRAT_BIN` to an absolute binary path to override the default. Both browser scripts set `WOMPRAT_HEADLESS=1` and `WOMPRAT_DIRECT=1`; release builds ignore the direct-dial bypass.

## Shell tests

`ux.mjs` starts local HTTP and RFB fixtures and checks:

- Settings, SSH, VNC and RDP panel creation;
- VNC authentication and reconnect behaviour;
- managed download contents;
- stable tab order, titles and close targets;
- error-bar hit targets and controls;
- DNS form alignment at 894px and 540px, disconnected-state reporting and clipboard copying;
- the Recent grid, removal without closing tabs, and saved dismissal state in a fresh page;
- terminal font loading;
- absence of browser page and console errors.

To save screenshots of the DNS form during the test:

```bash
mkdir -p dist/ux-layout
WOMPRAT_UX_SCREENSHOTS="$PWD/dist/ux-layout" make ux-test
```

The directory receives `dns-form-wide.png` and `dns-form-narrow.png`.

`make frontend-test` runs browser-independent behavioural tests. These include the ten-item Recent display limit, tab-save ordering, terminal controls, VNC lifecycle and RDP resizing. `make verify` includes these tests, frontend bundle checks, Go tests, Windows ARM64 vet and both Windows compile checks.

## Real VNC and RDP servers

`real-remotes.mjs` requires reachable test servers. Its defaults are VNC at `vnc://127.0.0.1:5902` and RDP at `rdp://127.0.0.1:3389`. Supply dedicated test credentials through environment variables; do not use production accounts.

```bash
export RDP_USER='test-account'
read -rs -p 'RDP test password: ' RDP_PASS; export RDP_PASS; echo
PLAYWRIGHT_BROWSERS_PATH=$HOME/.cache/ms-playwright bun run tests/ux/real-remotes.mjs
```

Use `WOMPRAT_UX_SKIP_VNC=1` or `WOMPRAT_UX_SKIP_RDP=1` to run one protocol. `VNC_TARGET`, `RDP_TARGET`, `RDP_USER` and `RDP_PASS` override the corresponding defaults.

The RDP script fills the credential dialog, waits for canvas output and samples non-uniform pixels. After resize, it checks the WebSocket identity, viewport coverage, credential-dialog visibility and centre-coordinate mapping. When the server supports Display Control, it also checks the backing canvas size. The VNC script waits for server-reported dimensions and samples canvas pixels.

Screenshots are written to `dist/ux-artifacts/rdp-browser-proof.png` and `dist/ux-artifacts/vnc-browser-proof.png`. Set `WOMPRAT_UX_ARTIFACTS` to use another directory.

A disposable local VNC target can be started with:

```bash
Xvfb :78 -screen 0 1024x768x24 &
DISPLAY=:78 openbox &
DISPLAY=:78 xterm -geometry 80x20+120+120 -fa Monospace -fs 18 &
x11vnc -display :78 -rfbport 5902 -localhost -forever -shared -nopw -quiet &
```

The unauthenticated VNC example binds only to loopback. Stop the test processes after use. For RDP, configure a dedicated loopback-accessible XRDP instance and test user.

## DNS and tailnet checks

```bash
go test -race -count=1 -timeout 180s ./...
go test tailscale.com/ipn/ipnlocal -run '^TestDNSConfigForNetmapForExitNodeConfigs$' -count=1
```

The second command runs the upstream policy regression and can require upstream test dependencies. It tests configured global/split resolvers with and without exit-node eligibility. App tests cover local MagicDNS records, DNS packets, timeout/NXDOMAIN reporting, policy descriptions and refusal of routes that could use host networking.

These fixtures do not authenticate to a real tailnet. To check a deployed configuration, run the Windows app, select the required exit node, and use **Settings → Diagnostics → Look up**. Verify both the resolver/route and returned addresses. Then open the intended service to test connectivity. See [DNS configuration and troubleshooting](../../docs/dns-exit-node.md).

Both browser scripts exit non-zero on assertion failures or browser page/console errors. Windows COM tests run separately in the release workflow before asset publication.
