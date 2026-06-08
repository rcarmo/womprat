# womprat

Single-binary portable terminal + browser over Tailscale for Windows ARM64.

## What it does

- Embeds Tailscale via `tsnet` — no Tailscale client install needed
- SSH to any peer in your tailnet via xterm.js terminal tabs
- Browse web UIs on your tailnet peers (piclaw, Proxmox, Gitea, etc.)
- Auth key prompted on first run, saved to system keychain
- Multiple tabs — mix terminals and browsers

## Architecture

```
┌──────────────────────────────────┐
│         womprat.exe (~20MB)      │
│                                  │
│  Go backend:                     │
│    tsnet (embedded tailscale)    │
│    SSH client (x/crypto/ssh)     │
│    HTTP server (localhost)       │
│    System keychain (go-keyring)  │
│                                  │
│  Frontend (embed.FS):            │
│    xterm.js + addons             │
│    Tab manager                   │
│    Peer discovery UI             │
│                                  │
│  Window: WebView2 (system)       │
└──────────────────────────────────┘
```

## Build

```bash
# Native (current OS)
go build -o womprat .

# Windows ARM64
GOOS=windows GOARCH=arm64 go build -o womprat.exe .
```

## First Run

1. Launch `womprat.exe`
2. Enter your Tailscale auth key (`tskey-auth-...`)
3. Key is saved to Windows Credential Manager
4. Peers appear — click SSH or Web to open tabs

## Dependencies

| Library | Purpose |
|---|---|
| `tailscale.com/tsnet` | Embedded Tailscale |
| `github.com/jchv/go-webview2` | WebView2 window (no CGO) |
| `github.com/zalando/go-keyring` | System keychain (Win Credential Manager) |
| `golang.org/x/crypto/ssh` | SSH client |
| `nhooyr.io/websocket` | WebSocket for terminal I/O |
