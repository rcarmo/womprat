# womprat

Single-binary portable terminal + browser over Tailscale for Windows ARM64.

## What it does

- Embeds Tailscale via `tsnet` — no Tailscale client install needed
- SSH to any peer in your tailnet via xterm.js terminal tabs
- Browse web UIs on your tailnet peers (piclaw, Proxmox, Gitea, etc.)
- All settings managed in-app (no config files, no terminal needed)
- Secrets encrypted at rest with DPAPI + optional Windows Hello biometric unlock
- Multiple tabs — mix terminals and browsers

## Security Model

Since this runs as a standalone GUI app (no terminal, no shell), it manages its own secrets:

```
┌─────────────────────────────────────────────────┐
│  Windows Credential Manager (DPAPI-backed)      │
│                                                 │
│  womprat/tailscale-key  → encrypted auth key    │
│  womprat/ssh/<host>     → encrypted SSH keys    │
│  womprat/master         → master key (optional) │
└─────────────────────────────────────────────────┘
         │
         ▼  unlocked by one of:
  ┌──────────────────────────┐
  │ Windows Hello (face/pin) │  ← preferred
  ├──────────────────────────┤
  │ Master password          │  ← fallback
  ├──────────────────────────┤
  │ DPAPI user-scope         │  ← transparent (weakest)
  └──────────────────────────┘
```

### First Run
1. App launches → settings screen (no terminal)
2. User chooses unlock method: Windows Hello PIN/biometric OR master password
3. User pastes Tailscale auth key → encrypted + stored
4. User can import SSH keys (file picker) → encrypted + stored
5. App connects to tailnet, shows peers

### Subsequent Runs
1. App launches → unlock prompt (Hello/PIN or master password)
2. Secrets decrypted → tsnet connects → ready

## Architecture

```
┌──────────────────────────────────────┐
│          womprat.exe (~20MB)         │
│                                      │
│  Settings Manager:                   │
│    - Tailscale auth key              │
│    - SSH keys per host               │
│    - Default user per host           │
│    - Window size/position            │
│    - Last-used tabs                  │
│                                      │
│  Credential Store:                   │
│    - Windows Credential Manager      │
│    - DPAPI encryption                │
│    - Windows Hello integration       │
│    - OR master password (PBKDF2)     │
│                                      │
│  Networking:                         │
│    - tsnet (embedded tailscale)      │
│    - SSH client (x/crypto/ssh)       │
│    - HTTP proxy for browser tabs     │
│                                      │
│  UI: WebView2 (system-provided)      │
│    - Tab manager                     │
│    - xterm.js terminals              │
│    - Browser frames                  │
│    - Settings pane (in-app)          │
│    - Peer discovery                  │
└──────────────────────────────────────┘
```

## Settings (all managed in-app via GUI)

| Setting | Storage | Notes |
|---|---|---|
| Tailscale auth key | Windows Credential Manager | Encrypted with DPAPI |
| SSH private keys | Windows Credential Manager | Per-host, imported via file picker |
| Default SSH user per host | App config (JSON, DPAPI-encrypted) | |
| Window geometry | App config | Restored on launch |
| Open tabs on exit | App config | Session restore |
| Unlock method | App config | `hello` or `master` |
| Master password hash | Windows Credential Manager | PBKDF2 for verification |

App config file: `%APPDATA%/womprat/config.enc` (DPAPI-encrypted JSON)

## Build

```bash
# Windows ARM64
GOOS=windows GOARCH=arm64 go build -ldflags="-H windowsgui" -o womprat.exe .

# The -H windowsgui flag prevents a console window from appearing
```

## Dependencies

| Library | Purpose |
|---|---|
| `tailscale.com/tsnet` | Embedded Tailscale |
| `github.com/jchv/go-webview2` | WebView2 window (no CGO) |
| `github.com/danieljoos/wincred` | Windows Credential Manager |
| `github.com/billgraziano/dpapi` | DPAPI encryption |
| `golang.org/x/crypto/ssh` | SSH client |
| `nhooyr.io/websocket` | WebSocket for terminal I/O |

## Key Design Decisions

1. **No terminal/shell required** — app is `-H windowsgui`, launched from Start Menu or shortcut
2. **No config files in user-visible locations** — everything in `%APPDATA%/womprat/` encrypted
3. **No SSH agent dependency** — manages its own keys (imported via GUI file picker)
4. **No Tailscale client install** — tsnet embeds everything in the binary
5. **Session restore** — reopens last tabs on launch
6. **Browser tabs route through tsnet** — HTTP proxy so iframe can reach tailnet hosts
