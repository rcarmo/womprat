# womprat

![womprat icon](docs/icon-256.png)

`womprat` is a portable Windows (ARM64 and Intel) SSH terminal, browser, VNC viewer, and RDP viewer for your tailnet. It embeds Tailscale with `tsnet`, opens SSH sessions in tabbed terminals, provides native WebView2 browser tabs, and bridges VNC/RDP sessions through the same Tailscale connection, so you can reach machines, remote desktops, and web UIs on your tailnet from anywhere without installing the full Tailscale client.

This is meant to be dead simple: copy one executable, launch it, unlock your saved configuration, and get to the things that normally require a VPN client, a browser, an SSH client, and a pile of local setup.

![womprat browser and SSH tabs](docs/screenshot.webp)

## Why

I kept finding myself in places I couldn't install the full Tailscale client but needed it to access my own stuff. And sometimes you want access to a handful of machines without adding another persistent system service, changing the host network stack, or asking Windows to remember one more thing at boot.

And then I got a corporate, locked-down Windows ARM laptop to test and realized that I really wanted to get at my Proxmox cluster from outside the house.

`womprat` takes the opposite approach: the tailnet identity belongs to the app, not the machine. When it is running, it can reach your tailnet. When it is closed, there is no VPN client left behind.

That makes it useful for portable operations work, especially when SSH and internal web UIs are the only things you need.

> **Note:** Right now, configuration is encrypted but stored in %APPDATA%. Future passes will tackle running this straight off a USB stick, once the UX is a bit more stable.

Oh, and the name was, weirdly, the first Star Wars/Death Star trench-related thing that came to me. It was late.

## How networking works

The app starts an embedded Tailscale node through `tailscale.com/tsnet` and uses it for application traffic:

* SSH connections are dialled through `tsnet`.
* Browser traffic is sent to a local SOCKS5 endpoint.
* VNC and RDP WebSocket bridges dial their target hosts through `tsnet` and stream framebuffer/input data to the local shell.
* The SOCKS5 endpoint resolves and dials through `tsnet`, including public names, MagicDNS names, `.ts.net` names, `.local` aliases (if you use [`mdnsbridge`](https://github.com/rcarmo/mdnsbridge)), LAN names, and raw IPs.
* If exit-node routing is configured, `tsnet` lets you reach the open internet (also very handy if you end up on a restricted network).

## What is in the binary

The executable contains the app shell, settings UI, tab manager, SSH terminal plumbing, SOCKS bridge, VNC/RDP viewers, embedded Tailscale client, and system WebView2 integration code. It does not bundle a browser engine -- it uses the Microsoft Edge WebView2 runtime already present on current Windows systems.

The main pieces are:

* `tsnet` for joining and routing over the tailnet.
* WebView2 for the native Windows browser window.
* `xterm.js` for SSH terminal tabs.
* Go's SSH stack for terminal sessions.
* A WASM-backed VNC framebuffer pipeline with Raw/Hextile/CopyRect/ZRLE/RRE/CoRRE support.
* A local `go-rdp` replacement for RDP connection, licensing, capabilities, and bitmap/surface update handling.
* Windows DPAPI for encrypting local configuration and credentials.
* A local HTTP API for the app shell, settings, tab state, terminal WebSockets, VNC WebSockets, and RDP WebSockets.

## Remote display tabs

VNC and RDP targets can be opened from the URL bar with normal custom URLs:

```text
vnc://host:5900
rdp://user@host:3389
```

Remote display tabs use a canvas-only workspace. The active session name and negotiated dimensions are promoted into the tab title, for example `sandbox:78 · 1024×768` or `rdp://host:3389 · 1280×720`, instead of taking space inside the canvas area.

### VNC

The VNC client intentionally negotiates Raw first for correctness across real servers that black-screen with some compressed encodings, then advertises faster/fallback encodings:

```text
Raw → Hextile → CopyRect → ZRLE → RRE → CoRRE → Cursor → ExtendedDesktopSize → DesktopSize → DesktopName → LastRect
```

VNC input supports pointer, wheel, clipboard, bounded cursor/desktop-name data, keypad keysyms, F1-F24, Meta/OS keysyms for NeXT-like targets, and active-key release on blur/reconnect/dispose to avoid stuck modifiers.

### RDP

RDP credentials are entered in a centered dialog. Once Connect is pressed, the dialog hides and the canvas is displayed. Status is shown in an auto-sized bottom-left bar styled like the browser status bar. Fit-to-viewport is the default display mode.

The default RDP path uses the performance profile and advertises WASM-backed surface/bitmap codecs. For compatibility with servers that need conservative bitmap updates, use an RDP URL/query path that reaches the WebSocket with `rfx=off`, `rfx=false`, `rfx=0`, or `compat=1`.

## Configuration and secrets

User data lives (for now) under:

```text
%APPDATA%\womprat\
```

The app stores its configuration as encrypted local state and keeps credentials protected with Windows DPAPI. Current unlock modes are intentionally simple:

* DPAPI user-scope unlock for normal per-user use.
* Master-password unlock for an explicit additional gate.

SSH host keys are pinned on first use rather than accepted blindly every time, which keeps the first-run experience tolerable without pretending that SSH host verification does not matter.

## What it does not do

`womprat` is not trying to replace the full Tailscale client for general-purpose system networking. It will not make every application on the machine see the tailnet, advertise routes, or act as a machine-wide VPN.

It also does not provide a generic proxy service--the entire point of this is that this is a single-purpose, self contained app.

## Building

The project Makefile is the supported build entry point:

```bash
make doctor
make setup
make verify
make windows-arm64
```

For a clean end-to-end build:

```bash
make release
```

The default target is documented with:

```bash
make help
```

The Windows ARM64 build emits:

```text
dist/womprat-windows-arm64.exe
```

The GUI build uses `-H windowsgui` so launching the executable does not create a console window.

## Build dependencies

You need:

* Go, with Windows ARM64 cross-compilation support.
* Bun, used to sanity-check the embedded HTML/JavaScript entry points.
* `llvm-windres`, used to generate the Windows ARM64 resource object.
* Python 3, currently used as a general project scripting dependency.

The `Makefile` checks these with `make doctor` and regenerates Windows `.syso` resource objects from the checked-in icon and manifest.

## Repository layout

```text
womprat/
├── cmd/
│   └── womprat/              Application entry point (package main)
│       ├── main.go           WebView2 shell, tab model, browser bindings
│       ├── gui_windows.go    Native dual-WebView host window (Windows)
│       ├── socks.go          Tailscale-backed SOCKS5 endpoint
│       ├── vnc.go            VNC WebSocket-to-TCP bridge through tsnet
│       ├── rdp.go            RDP WebSocket bridge through tsnet
│       ├── ws_terminal.go    SSH terminal WebSocket bridge
│       ├── settings_api.go   Settings HTTP API
│       ├── config.go         Encrypted app configuration model
│       ├── frontend/         HTML, CSS, and JavaScript for the app shell/settings
│       └── winres/           Windows icon resource inputs
├── internal/
│   └── go-webview2/          Vendored WebView2 wrapper (local replace) with patches
├── third_party/
│   └── go-rdp/               Local RDP module replacement used by go.mod
├── tests/
│   └── ux/                   Playwright shell and real-remote UX smoke tests
├── docs/
│   ├── icon.png             Source application icon
│   └── icon-256.png         README-sized icon
└── Makefile                 Full setup/check/resource/build pipeline
```

## Current target

The current target platform is Windows ARM64. Other build targets exist mostly as compile checks; the useful artefact is the single Windows ARM64 `womprat.exe`.

## Roadmap

* Make this a fully portable, USB keychain-style app
* Continue hardening RDP compatibility/performance across real servers
* Possibly do a Mac version
