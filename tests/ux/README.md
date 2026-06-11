# Linux UX test (Playwright)

Drives the real womprat shell in headless Chromium against a running app
instance (headless server mode + direct-dial bypass), using a built-in RFB
(VNC) stub so the VNC viewer connects end to end. Exercises the same frontend
URL-validation paths and SSH/VNC/RDP viewers used on Windows, and fails on any
console/page error.

## Run

```bash
# 1) Build a debug headless binary (enables WOMPRAT_DIRECT bypass)
go build -ldflags='-X main.debugBuild=1' -o dist/womprat-linux-debug ./cmd/womprat

# 2) Install Playwright + Chromium (once)
cd tests/ux && bun add -d playwright && bun x playwright install chromium

# 3) Run
PLAYWRIGHT_BROWSERS_PATH=$HOME/.cache/ms-playwright bun run ux.mjs
```

Exit code is non-zero if any check fails. Set WOMPRAT_BIN to override the binary.
