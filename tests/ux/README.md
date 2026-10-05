# Browser and network tests

The Playwright tests run Womprat's Linux headless shell in Chromium. They cover frontend layout, interaction and protocol bridges. They do not instantiate the native Windows WebView2 host.

## Set up

From the repository root:

```bash
make ux-setup
make ux-test
```

`make ux-test` builds `build/dist/womprat-linux-debug` beneath the resolved project temp root, runs `tests/ux/ux.mjs`, and runs the dynamic-title reporter test. Chromium uses `cache/playwright` under that root. CPU/heap profiles, logs and the matching server binary are retained under `evidence/tests/`.

The default local root is `/workspace/tmp/womprat`, falling back to platform temp plus `/womprat`. Set an absolute `PROJECT_TMP_BASE` to choose another base. CI prefers `RUNNER_TEMP`, then the original `TMPDIR`, then platform temp. See [AGENTS.md](../../AGENTS.md) for environment variables, override validation and cleanup rules.

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
mkdir -p evidence/ux-layout
WOMPRAT_UX_SCREENSHOTS="$PWD/evidence/ux-layout" make ux-test
```

The directory receives `dns-form-wide.png` and `dns-form-narrow.png`.

`make frontend-test` runs browser-independent behavioural tests. These include the ten-item Recent display limit, tab-save ordering, terminal controls, VNC lifecycle and RDP resizing. `make verify` includes these tests, frontend bundle checks, Go tests, Windows ARM64 vet and both Windows compile checks.

## Real VNC and RDP servers

`real-remotes.mjs` requires reachable test servers. Its defaults are VNC at `vnc://127.0.0.1:5902` and RDP at `rdp://127.0.0.1:3389`. Supply dedicated test credentials through environment variables; do not use production accounts.

```bash
export RDP_USER='test-account'
read -rs -p 'RDP test password: ' RDP_PASS; export RDP_PASS; echo
source scripts/paths.sh
WOMPRAT_BIN="$WOMPRAT_BUILD_DIR/dist/womprat-linux-debug" \
  bash scripts/bun-profile.sh real-remotes run tests/ux/real-remotes.mjs
```

Use `WOMPRAT_UX_SKIP_VNC=1` or `WOMPRAT_UX_SKIP_RDP=1` to run one protocol. `VNC_TARGET`, `RDP_TARGET`, `RDP_USER` and `RDP_PASS` override the corresponding defaults.

The RDP script fills the credential dialog, waits for canvas output and samples non-uniform pixels. After resize, it checks the WebSocket identity, viewport coverage, credential-dialog visibility and centre-coordinate mapping. When the server supports Display Control, it also checks the backing canvas size. The VNC script waits for server-reported dimensions and samples canvas pixels.

Screenshots `rdp-browser-proof.png` and `vnc-browser-proof.png` are retained with the run's profiles. `WOMPRAT_UX_ARTIFACT_DIR` selects another retained evidence directory.

Use a separately managed local display/VNC server. Xvfb creates host `/tmp` sockets even when `TMPDIR` is set, so launching it requires a path exception. For RDP, configure a dedicated loopback-accessible XRDP instance and test user.

## DNS and tailnet checks

```bash
make test-race
make test TEST_PACKAGES=tailscale.com/ipn/ipnlocal \
  TEST_FLAGS="-run=^TestDNSConfigForNetmapForExitNodeConfigs$ -count=20"
```

The second command runs the upstream policy regression and can require upstream test dependencies. It tests configured global/split resolvers with and without exit-node eligibility. App tests cover local MagicDNS records, DNS packets, timeout/NXDOMAIN reporting, policy descriptions and refusal of routes that could use host networking.

These fixtures do not authenticate to a real tailnet. To check a deployed configuration, run the Windows app, select the required exit node, and use **Settings → Diagnostics → Look up**. Verify both the resolver/route and returned addresses. Then open the intended service to test connectivity. See [DNS configuration and troubleshooting](../../docs/dns-exit-node.md).

Both browser scripts exit non-zero on assertion failures or browser page/console errors. Windows COM tests run separately in the release workflow before asset publication.
