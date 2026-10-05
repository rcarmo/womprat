#!/usr/bin/env bash
# Linux end-to-end debug harness for womprat.
#
# The womprat GUI is Windows-only (WebView2), but on non-Windows builds the
# binary keeps the local shell/API HTTP server running. This script launches the
# Linux binary, uses an existing display, opens the shell in a real browser, and
# leaves the environment up so you can automate it with xdotool / Playwright and
# debug the frontend + SSH/VNC/RDP/settings flows end to end.
#
# Usage:
#   scripts/linux-debug.sh [--display :99] [--browser chromium] [--no-browser]
#
# Environment:
#   WOMPRAT_BIN   path to the Linux binary (default project build/dist)
#   DISPLAY       existing display; this harness never creates host /tmp X11 sockets
#
# Outputs WOMPRAT_SHELL_URL / WOMPRAT_TOKEN (captured from the binary) and the
# existing DISPLAY so other tooling can attach.

set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/paths.sh"
DISPLAY_NUM="${DISPLAY:-:99}"
BROWSER_BIN=""
OPEN_BROWSER=1
WOMPRAT_BIN="${WOMPRAT_BIN:-$WOMPRAT_BUILD_DIR/dist/womprat-linux-amd64}"

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

command -v xdpyinfo >/dev/null || { echo 'xdpyinfo required to validate existing display' >&2; exit 1; }
xdpyinfo -display "$DISPLAY_NUM" >/dev/null 2>&1 || { echo 'An existing display is required; launching Xvfb writes host /tmp sockets and needs an explicit path exception.' >&2; exit 1; }
command -v xdotool >/dev/null || echo "warning: xdotool not installed (sudo apt install xdotool)" >&2

if [ -z "$BROWSER_BIN" ] && [ "$OPEN_BROWSER" = "1" ]; then
  for c in chromium chromium-browser google-chrome firefox; do
    if command -v "$c" >/dev/null; then BROWSER_BIN="$c"; break; fi
  done
fi

mkdir -p "$WOMPRAT_EVIDENCE_ROOT/debug"
LOG_DIR="$(mktemp -d "$WOMPRAT_EVIDENCE_ROOT/debug/$(date -u +%Y%m%dT%H%M%S)-XXXXXX")"
DEBUG_HOME="$(mktemp -d "$WOMPRAT_RUN_DIR/home-XXXXXX")"
export HOME="$DEBUG_HOME" XDG_CONFIG_HOME="$DEBUG_HOME/.config" XDG_DATA_HOME="$DEBUG_HOME/.local/share"
mkdir -p "$DEBUG_HOME/ff"
echo "debug logs: $LOG_DIR"
export DISPLAY="$DISPLAY_NUM"

cleanup() {
  set +e
  [ -n "${WOMPRAT_PID:-}" ] && kill "$WOMPRAT_PID" 2>/dev/null
  [ -n "${BROWSER_PID:-}" ] && kill "$BROWSER_PID" 2>/dev/null
  # The existing display belongs to its caller; never terminate it.
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

# 3) Optionally open the shell in a browser on the existing display.
if [ "$OPEN_BROWSER" = "1" ] && [ -n "$BROWSER_BIN" ]; then
  case "$BROWSER_BIN" in
    chromium*|google-chrome*)
      "$BROWSER_BIN" --no-first-run --no-default-browser-check \
        --user-data-dir="$DEBUG_HOME/chrome" "$SHELL_URL" >"$LOG_DIR/browser.log" 2>&1 &
      ;;
    firefox*)
      "$BROWSER_BIN" --no-remote --profile "$DEBUG_HOME/ff" "$SHELL_URL" >"$LOG_DIR/browser.log" 2>&1 &
      ;;
    *)
      "$BROWSER_BIN" "$SHELL_URL" >"$LOG_DIR/browser.log" 2>&1 &
      ;;
  esac
  BROWSER_PID=$!
  echo "browser ($BROWSER_BIN) pid=$BROWSER_PID on $DISPLAY"
  echo "automate with e.g.: DISPLAY=$DISPLAY xdotool search --name womprat"
fi

echo "Environment is up. Press Ctrl-C to stop womprat and the browser (existing display is preserved)."
wait "$WOMPRAT_PID"
