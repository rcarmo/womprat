# Terminals, remote desktops and browser data

See the [README][readme] for downloads, first connection, shortcuts and security warnings. This reference covers protocol behaviour and settings.

## SSH terminals

Open `ssh://user@host:22` from the address bar. Womprat tries the key assigned to the host, then other stored keys, then prompts for a password if key authentication fails. SSH host keys use trust on first use: the first key is pinned in the host profile and a later mismatch is rejected.

Removing a host clears its local URL, SSH-key association and pinned host key. It does not delete the named private key, which other hosts may share, or remove a device from the Tailscale control plane.

Terminals use xterm.js. Settings offers bundled FiraCode Nerd Font Mono, Cascadia Mono, Consolas and NSimSun/SimSun; the latter three require installed fonts. Cascadia and Consolas fall back to bundled FiraCode before the CJK fonts. Font loading finishes before measuring terminal cells; changing fonts refits existing sessions.

Ordinary control chords go to the remote shell. Use `Ctrl+Shift+T` for a new tab, `Ctrl+Shift+W` to close it and `Ctrl+Shift+L` for the address bar. Next/previous tab use `Ctrl+Shift+PageDown` / `Ctrl+Shift+PageUp`; `Ctrl+Shift+Tab` also selects the next tab when a terminal is active. `Ctrl+Shift+1` ... `Ctrl+Shift+9` select tabs by position. `Ctrl+C` copies selected text; without a selection it sends ETX. The shell's context menu also supports terminal copy and paste on Windows.

## Browser tabs and downloads

External HTTP/HTTPS content uses native WebView2 views on Windows and WebKitGTK views on Linux. Shell pages stay on the authenticated loopback origin. URL, title, favicon, history availability, tab order and loading/error state are synchronised with the shell. Failed navigation offers **Reload**, **Settings** and **Dismiss**.

Links with `target=_blank`, common `window.open()` calls and native popup requests open a Womprat tab by navigating to a URL. Popup POST bodies and opener-window JavaScript relationships are not retained.

The managed downloader accepts HTTP/HTTPS, uses tsnet, saves under the current user's `Downloads` directory, sanitises filenames and chooses an unused name. It creates files exclusively, removes incomplete files and runs one download at a time.

A native-tab download can inherit cookies selected for a same-origin target, including HttpOnly cookies. The browser cookie values remain in Go memory behind a random, one-use ticket that expires after 30 seconds; they are not exposed to page/shell JavaScript, URLs, logs or status responses. Inherited cookies are host-only during redirect handling. Cross-origin links and direct API downloads start without inherited browser cookies.

Settings can clear cache, one cookie domain, all cookies or all browsing data. A domain selector matches the exact host and its subdomains. Windows also supports browser password saving and deletion; Linux browser password saving is not implemented.

### HTTPS certificate exceptions

An eligible untrusted, expired or hostname-mismatched certificate can produce a **Proceed anyway** button after the native browser reports the certificate. Consent is bound to the tab, URL and certificate before approval. Approval then applies to that host/certificate for the shared browser session, including other tabs. Restart to clear approvals; a different certificate needs fresh consent.

Generic load, DNS and connection errors cannot enable the exception. Revoked and client-certificate errors cannot be bypassed. Windows requires an up-to-date WebView2 runtime. Linux reports certificate errors through WebKitGTK.

## VNC

Open `vnc://host:5900`. Womprat supports RFB `None` and classic VNC password authentication. Password-required servers open an in-tab dialog; the password is used for that connection and is not saved. Other RFB security extensions are not implemented.

Supported framebuffer encodings are Raw, Hextile, CopyRect, ZRLE, RRE and CoRRE, with cursor, desktop-size, extended-desktop-size, desktop-name and LastRect pseudo-encodings. Raw is offered first for compatibility with the tested servers.

Input includes pointer, wheel, clipboard, keypad keys, F1-F24 and Meta/OS keys. Active keys are released on blur, reconnect and disposal. Reconnects discard callbacks from older connections so they cannot reset a newer session.

## RDP

Open `rdp://user@host:3389`. Enter credentials in the tab; passwords do not belong in the URL. The client advertises WASM-backed NSCodec, RemoteFX, RemoteFX-Image and bitmap decoding; the server determines the negotiated set. H.264 graphics are not implemented.

The initial desktop size uses the visible viewport. Servers with MS-RDPEDISP Display Control can resize the remote desktop as the window changes, without reconnecting. Otherwise Womprat fits the existing framebuffer to the viewport and scales pointer coordinates accordingly. The query options `rfx=off`, `rfx=false`, `rfx=0` or `compat=1` disable the performance codec path for bitmap-only compatibility.

**RDP server certificates are not verified.** TLS encryption, when negotiated, does not authenticate the target. Use RDP across a trusted tailnet and verify the server independently.

## Saved sessions and Recent

**Restore tabs** reopens saved tabs after startup or master-password unlock. It excludes blank tabs and Settings. Browser views are created lazily when selected; a restored terminal prompts or authenticates through the usual SSH path.

Home shows up to 10 Recent cards, derived from saved session tabs. Removing a card retains the open tab and its restore state. The saved dismissal list is capped at 100 keys. Address-bar history is separate: it stores up to 100 submitted addresses or searches for local completion.

## Local storage and diagnostics

Configuration and credentials use Windows user-scoped DPAPI or Linux AES-GCM with a private `.keyfile` in the configuration directory. The master password adds an application access check; it does not change the underlying encryption. Browser profile data is separate. Linux WebKit profile data is not encrypted by Womprat.

Configuration/credential updates write a private temporary file, flush and close it, then replace the previous file. Settings, tab snapshots and host-key pinning serialise their read/modify/write operations. This does not make the state portable between machines; USB-contained state is not implemented.

Debug logging is off by default. Windows writes `womprat-log.txt` beside the executable when enabled, enables developer tools for newly created views and shows an attached console only when Womprat owns it. Linux AppImages mount the executable directory read-only; a writable persistent file log and the Windows developer-tool behaviour are not implemented for that package. Do not rely on the debug toggle to produce an AppImage log.

The HTTP server and proxies bind to loopback. Protected API calls need the session token and the HTTP server rejects foreign Host headers. SOCKS and Linux content proxies do not authenticate local clients; local software can use them explicitly while the application is connected. Windows grants shell clipboard-read permission and denies permission requests whose kind cannot be read. Linux denies native content permission requests.

DNS diagnostics can show nameservers, split-DNS selection, route, transport and response details without changing tailnet policy. Reports expose private hostnames and IP addresses; review them before sharing. [DNS troubleshooting][dns] covers supported resolver transports and result interpretation.

[readme]: ../README.md
[dns]: dns-exit-node.md
