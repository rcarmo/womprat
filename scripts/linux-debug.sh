#!/usr/bin/env bash
# Linux end-to-end debug harness for womprat.
#
# The womprat GUI is Windows-only (WebView2), but on non-Windows builds the
# binary keeps the local shell/API HTTP server running. This script launches the
# Linux binary, starts an Xvfb display, opens the shell in a real browser, and
# leaves the environment up so you can automate it with xdotool / Playwright and
# debug the frontend + SSH/VNC/RDP/settings flows end to end.
#
# Usage:
#   scripts/linux-debug.sh [--display :99] [--browser chromium] [--no-browser]
#
# Environment:
#   WOMPRAT_BIN   path to the linux binary (default dist/womprat-linux-amd64)
#   XVFB_RES      Xvfb resolution (default 1280x900x24)
#
# Outputs WOMPRAT_SHELL_URL / WOMPRAT_TOKEN (captured from the binary) and the
# Xvfb DISPLAY so other tooling can attach.

set -euo pipefail

DISPLAY_NUM=":99"
BROWSER_BIN=""
OPEN_BROWSER=1
XVFB_RES="${XVFB_RES:-1280x900x24}"
WOMPRAT_BIN="${WOMPRAT_BIN:-dist/womprat-linux-amd64}"

while [ $# -gt 0 ]; do
  case "$1" in
    --display) DISPLAY_NUM="$2"; shift 2 ;;
    --browser) BROWSER_BIN="$2"; shift 2 ;;
    --no-browser) OPEN_BROWSER=0; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [ ! -x "$WOMPRAT_BIN" ]; then
  echo "womprat binary not found/executable at $WOMPRAT_BIN (build with: make linux)" >&2
  exit 1
fi

command -v Xvfb >/dev/null || { echo "Xvfb not installed (sudo apt install xvfb)" >&2; exit 1; }
command -v xdotool >/dev/null || echo "warning: xdotool not installed (sudo apt install xdotool)" >&2

if [ -z "$BROWSER_BIN" ] && [ "$OPEN_BROWSER" = "1" ]; then
  for c in chromium chromium-browser google-chrome firefox; do
    if command -v "$c" >/dev/null; then BROWSER_BIN="$c"; break; fi
  done
fi

LOG_DIR="$(mktemp -d)"
echo "debug logs: $LOG_DIR"

# 1) Start Xvfb.
Xvfb "$DISPLAY_NUM" -screen 0 "$XVFB_RES" >"$LOG_DIR/xvfb.log" 2>&1 &
XVFB_PID=$!
export DISPLAY="$DISPLAY_NUM"
sleep 1

cleanup() {
  set +e
  [ -n "${WOMPRAT_PID:-}" ] && kill "$WOMPRAT_PID" 2>/dev/null
  [ -n "${BROWSER_PID:-}" ] && kill "$BROWSER_PID" 2>/dev/null
  kill "$XVFB_PID" 2>/dev/null
}
trap cleanup EXIT INT TERM

# 2) Start womprat (serves shell + API; prints WOMPRAT_SHELL_URL / WOMPRAT_TOKEN).
"$WOMPRAT_BIN" >"$LOG_DIR/womprat.log" 2>&1 &
WOMPRAT_PID=$!

SHELL_URL=""
for _ in $(seq 1 50); do
  SHELL_URL="$(grep -m1 '^WOMPRAT_SHELL_URL=' "$LOG_DIR/womprat.log" | cut -d= -f2- || true)"
  [ -n "$SHELL_URL" ] && break
  sleep 0.2
done
TOKEN="$(grep -m1 '^WOMPRAT_TOKEN=' "$LOG_DIR/womprat.log" | cut -d= -f2- || true)"

if [ -z "$SHELL_URL" ]; then
  echo "failed to capture shell URL; see $LOG_DIR/womprat.log" >&2
  cat "$LOG_DIR/womprat.log" >&2 || true
  exit 1
fi

echo "WOMPRAT_SHELL_URL=$SHELL_URL"
echo "WOMPRAT_TOKEN=$TOKEN"
echo "DISPLAY=$DISPLAY"
echo "womprat.log: $LOG_DIR/womprat.log"

# 3) Optionally open the shell in a browser on the Xvfb display.
if [ "$OPEN_BROWSER" = "1" ] && [ -n "$BROWSER_BIN" ]; then
  case "$BROWSER_BIN" in
    chromium*|google-chrome*)
      "$BROWSER_BIN" --no-first-run --no-default-browser-check \
        --user-data-dir="$LOG_DIR/chrome" "$SHELL_URL" >"$LOG_DIR/browser.log" 2>&1 &
      ;;
    firefox*)
      "$BROWSER_BIN" --no-remote --profile "$LOG_DIR/ff" "$SHELL_URL" >"$LOG_DIR/browser.log" 2>&1 &
      ;;
    *)
      "$BROWSER_BIN" "$SHELL_URL" >"$LOG_DIR/browser.log" 2>&1 &
      ;;
  esac
  BROWSER_PID=$!
  echo "browser ($BROWSER_BIN) pid=$BROWSER_PID on $DISPLAY"
  echo "automate with e.g.: DISPLAY=$DISPLAY xdotool search --name womprat"
fi

echo "Environment is up. Press Ctrl-C to tear down (Xvfb + womprat + browser)."
wait "$WOMPRAT_PID"
