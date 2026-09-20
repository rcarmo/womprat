# womprat

![womprat icon](docs/icon-256.png)

`womprat` is a single-binary Windows client for SSH terminals, web applications, VNC desktops and RDP desktops on a Tailscale network. It runs its own `tsnet` node, so it does not install a machine-wide VPN service or change the host network stack.

Windows ARM64 is the primary target. Windows AMD64 is also built and released.

![womprat browser and SSH tabs](docs/screenshot.webp)

## Purpose

I built Womprat for Windows machines where I cannot install the full Tailscale client but still need access to SSH hosts, Proxmox, internal web applications and remote desktops.

The tailnet identity belongs to the application. Closing Womprat closes its Tailscale node; other applications on the machine do not gain tailnet access.

Womprat stores its state under `%APPDATA%\womprat`. The executable can be copied between machines, but its encrypted state is tied to the Windows user account. USB-only operation is not implemented.

## Requirements

A Windows release needs:

* Windows on ARM64 or AMD64;
* the Microsoft Edge WebView2 runtime, which is present on current Windows 10 and Windows 11 installations;
* a Tailscale auth key that can register the embedded node;
* credentials for each SSH, VNC or RDP service you open.

Womprat does not require the system Tailscale client.

## Traffic paths

Womprat binds its shell, API and SOCKS listener to loopback. Application traffic follows these paths:

| Feature | Route |
| --- | --- |
| SSH | Go SSH client -> `tsnet` -> target |
| Browser | WebView2 -> loopback SOCKS5 -> `tsnet` -> target |
| Managed download | Go HTTP client -> `tsnet` -> target |
| VNC | local WebSocket bridge -> `tsnet` TCP connection -> target |
| RDP | local WebSocket bridge -> `tsnet` TCP connection -> target |

Release builds fail closed. If `tsnet` is unavailable, Womprat does not fall back to the host's normal network. `WOMPRAT_DIRECT=1` works only in binaries built with `-X main.debugBuild=1` and exists for local integration tests.

Public internet access requires a configured and active Tailscale exit node. Tailnet hosts, MagicDNS names, `.ts.net` names, IP addresses and names made available through a service such as [`mdnsbridge`](https://github.com/rcarmo/mdnsbridge) are resolved through `tsnet`.

Transient Tailscale startup failures are retried every 15 seconds. Settings reports the last error and distinguishes a configured exit node from one that is active in the current session. An explicit reconnect or disconnect cancels the existing retry worker before changing the connection.

## Tabs and shortcuts

The tab strip contains native browser views and shell-rendered terminal, VNC, RDP and Settings panels. A new tab is an address-bar placeholder; navigating it replaces that placeholder rather than leaving an empty tab behind. Dragging a tab before or after another tab preserves the same order in shell and persisted state.

Browser shortcuts:

| Shortcut | Action |
| --- | --- |
| `Ctrl+L` or `Alt+D` | Focus the address bar |
| `Ctrl+T` | New blank tab |
| `Ctrl+W` | Close active browser or remote-display tab |
| `Ctrl+Tab`, `Ctrl+PageDown` | Next tab |
| `Ctrl+Shift+Tab`, `Ctrl+PageUp` | Previous tab |
| `Ctrl+1` … `Ctrl+9` | Select tab |
| `Ctrl+R` or `F5` | Reload browser tab |
| `Alt+Left`, `Alt+Right` | Browser history |
| `Ctrl++`, `Ctrl+-`, `Ctrl+0` | Content zoom |

Terminal tabs reserve ordinary control chords for the remote shell. Shell-level tab actions use the corresponding `Ctrl+Shift` chord. `Ctrl+C` copies the current terminal selection; when there is no selection it sends ETX to interrupt the remote process. The shell WebView keeps its context menu enabled for terminal copy and paste.

Open tabs can be restored on launch. Blank tabs and Settings are not persisted. Protected shell state, recent tabs and terminal appearance load after the master-password gate has been unlocked.

## SSH terminals

Open an SSH tab from the address bar:

```text
ssh://user@host:22
```

SSH tabs use xterm.js and support configurable font size and these font choices:

* bundled FiraCode Nerd Font Mono;
* Cascadia Mono, if installed;
* Consolas, if installed;
* NSimSun/SimSun, if installed.

Font changes apply to existing terminal sessions and cause them to refit. Womprat tries the key assigned to a host first, then other stored keys. If key authentication fails, it prompts for a password.

SSH host keys use trust on first use. The first key is pinned in the host profile; a later mismatch is rejected. Removing a host removes its local URL, SSH association and pinned host key. It does not delete the named private key, because other hosts may share that credential, and it does not remove a device from the Tailscale control plane.

## Browser tabs and downloads

HTTP and HTTPS URLs open in native WebView2 child views. Womprat synchronises the live URL, title, favicon, history availability, zoom, tab order and restore state with the shell.

Links using `target=_blank`, common `window.open()` calls and native WebView2 popup requests open as Womprat tabs. Popup redirection creates a new URL navigation. It does not preserve a popup POST body or opener-window JavaScript relationship.

Download links handled by the shell use the managed downloader. It:

* accepts HTTP and HTTPS URLs;
* routes connections through `tsnet`;
* writes to the current user's `Downloads` directory;
* sanitises Windows filenames and chooses a new name instead of overwriting;
* creates files exclusively to avoid path races;
* removes incomplete files;
* runs one managed download at a time.

Managed downloads do not share WebView2 cookies. Downloads that require a browser-authenticated session may therefore fail.

Settings can clear cache, one cookie domain, all cookies, saved browser passwords or all browsing data. Domain deletion matches the exact host and its subdomains; it does not use a broad suffix match.

## VNC

Open a VNC tab with:

```text
vnc://host:5900
```

The client supports RFB `None` and classic VNC password authentication. A password-required server opens an in-tab password dialog. The password is used for that connection and is not persisted. Reconnects use connection generations so callbacks from an old socket cannot reset a newer session.

The decoder supports Raw, Hextile, CopyRect, ZRLE, RRE and CoRRE updates, plus cursor, desktop-size, extended-desktop-size, desktop-name and LastRect pseudo-encodings. Raw is offered first because it has proved more reliable across the test servers used for Womprat.

VNC input includes pointer, wheel, clipboard, keypad keys, F1-F24 and Meta/OS keysyms. Active keys are released on blur, reconnect and disposal to avoid stuck modifiers.

## RDP

Open an RDP tab with:

```text
rdp://user@host:3389
```

Credentials are entered in the tab and are not placed in the URL. The client advertises WASM-backed NSCodec, RemoteFX, RemoteFX-Image and bitmap decoding; the negotiated set depends on the server.

The initial desktop size uses the visible content viewport. If the server supports MS-RDPEDISP Display Control, later window changes request a matching remote desktop size without reconnecting. Otherwise the existing framebuffer is fitted to the viewport. Pointer coordinates independently invert horizontal and vertical CSS scaling.

The compatibility query parameters `rfx=off`, `rfx=false`, `rfx=0` and `compat=1` disable the performance codec path for servers that require conservative bitmap updates.

## Configuration and secrets

Windows state is stored below:

```text
%APPDATA%\womprat\
```

`config.enc` contains window state, open tabs, host profiles, appearance, exit-node choice and diagnostics preferences. Credential files are stored below `creds/`. Windows encrypts both configuration and credential files with user-scoped DPAPI. They are not Windows Credential Manager entries.

The WebView2 profile is stored below the same Womprat directory and uses WebView2's profile protection. Debug logs, when enabled, are written next to the executable.

Unlock modes:

* `dpapi` opens state for the current Windows user without another prompt;
* `master` adds a PBKDF2-SHA256 password check before protected API access and Tailscale startup.

The master password is an application gate. DPAPI remains the encryption mechanism for stored state.

Configuration and credential writes use a private temporary file, flush and close it, then replace the destination. Settings changes, tab persistence and host-key pinning serialise their read/modify/write transactions.

## Diagnostics

Settings includes checks for:

* Tailscale connectivity;
* the local SOCKS listener;
* public DNS and connection through SOCKS when an exit node is active.

The public probe is marked `Skipped` when no exit node is active. A configured exit node that failed to apply is reported as degraded state rather than as a working public route.

Debug logging is disabled by default. Enabling it writes `womprat-log.txt`, enables WebView developer tools for newly created views and shows an attached Windows console when Womprat owns that console. Disabling it closes the log file and hides only a console owned solely by the Womprat process.

## Security boundaries

Womprat's local HTTP server and SOCKS listener bind to loopback. API calls require a random per-process session token. The HTTP server rejects foreign `Host` headers before serving token-bearing HTML or API responses.

The application grants WebView2 clipboard-read permission for the shell. Permission requests whose kind cannot be read are denied. Browser process failures are surfaced in the affected tab with reload or restart guidance.

RDP currently disables certificate verification for the target RDP server. The transport is encrypted when TLS is negotiated, but Womprat does not authenticate the server certificate. Use RDP only across a trusted tailnet and verify the target independently.

## Limits

Womprat is an application client, not a machine-wide VPN. It does not advertise routes or expose the tailnet to other applications.

Current limits include:

* Windows is the supported desktop runtime; Linux targets exist for development and automated tests;
* managed downloads do not inherit browser cookies;
* popup POST bodies and opener relationships are not retained;
* VNC supports `None` and classic password authentication, not every RFB security extension;
* RDP H.264 graphics are not implemented;
* RDP server certificates are not verified;
* configuration remains under `%APPDATA%`; USB-contained state is not implemented.

## Build

The Makefile is the supported entry point:

```bash
make doctor
make setup
make verify
make windows-arm64
make windows-amd64
```

A clean ARM64 release build:

```bash
make release
```

An Intel/AMD x64 release build:

```bash
make release-intel
```

Outputs:

```text
dist/womprat-windows-arm64.exe
dist/womprat-windows-amd64.exe
```

Run `make sha256` after building to create `dist/SHA256SUMS.txt`. Both release targets use `-H windowsgui`, so they start without a console window.

Build dependencies:

* Go 1.25 or later;
* Bun;
* `llvm-windres`;
* Python 3.

`make doctor` checks the tools and required icon/manifest inputs. `make help` lists all targets.

## Tests

`make verify` runs:

* Bun bundle checks for the shell, Settings, VNC and RDP assets;
* server-independent frontend behavioural tests;
* host Go tests;
* Windows ARM64 `go vet`;
* Windows ARM64 and AMD64 compile checks.

Additional validation used by this repository:

```bash
go test -race ./...
make ux-test
bun run tests/ux/real-remotes.mjs
```

`tests/ux/ux.mjs` drives the shell in Chromium and covers downloads, tabs, Settings, SSH routing, VNC `None`, VNC password reconnect and RDP panel creation. `tests/ux/real-remotes.mjs` checks non-uniform framebuffer pixels and resized RDP geometry against real servers. See [`tests/ux/README.md`](tests/ux/README.md) for setup and test boundaries.

The release workflow executes the WebView2 COM regression tests on a Windows runner before it builds and publishes ARM64 and AMD64 binaries.

## Repository layout

```text
cmd/womprat/              application, APIs, protocol bridges and embedded frontend
internal/go-webview2/     local WebView2 wrapper and COM fixes
third_party/go-rdp/       local RDP module replacement
tests/ux/                 Playwright and frontend behavioural tests
docs/                     icons, screenshot and audit notes
Makefile                  build, verification and release targets
```
