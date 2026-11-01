#!/usr/bin/env bash
# Runs inside Fedora as a non-root user. Exact release has no test bridge;
# separate test-tagged binary supplies functional fixtures and Go captures.
set -Eeuo pipefail
trap 'status=$?;printf "Native runner failed at line %s: %s (exit %s)\\n" "$LINENO" "$BASH_COMMAND" "$status" >&2' ERR
image=$1 profiles=$2
cd "$WOMPRAT_RUN_DIR/source"
source scripts/paths.sh
printf 'PASS native runner paths initialised: %s\n' "$WOMPRAT_RUN_DIR"
[[ $(id -u) != 0 ]] || { echo 'Native acceptance must run as normal user' >&2;exit 1; }
export XDG_CONFIG_HOME="$HOME/.config" XDG_DATA_HOME="$HOME/.local/share"
mkdir -p "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$profiles"
# Exact AppImage execution uses the runtime extraction fallback. No FUSE needed.
# Deliberately set the debug env override: shipping build must ignore it.
WOMPRAT_DIRECT=1 APPIMAGE_EXTRACT_AND_RUN=1 dbus-run-session -- "$image" > "$WOMPRAT_RUN_DIR/release-gui.log" 2>&1 &
pid=$!
cleanup(){ local status=$?;if [[ $status != 0 ]];then for log in "$WOMPRAT_RUN_DIR"/{release,fixture,heap,cpu}-gui.log;do [[ ! -f $log ]]||tail -60 "$log" >&2;done;fi;kill "$pid" 2>/dev/null||true;wait "$pid" 2>/dev/null||true;}
trap cleanup EXIT INT TERM
for _ in $(seq 1 100);do grep -q '^WOMPRAT_SHELL_URL=' "$WOMPRAT_RUN_DIR/release-gui.log" && break;sleep .1;done
# Validate release renders a native window and embedded authenticated UI.
xdotool search --name Womprat >/dev/null || { cat "$WOMPRAT_RUN_DIR/release-gui.log" >&2;exit 1; }
bun tests/linux/release-smoke.mjs "$WOMPRAT_RUN_DIR/release-gui.log"
# Close via the window manager path, not SIGKILL.
window=$(xdotool search --name Womprat | tail -1)
xdotool windowclose "$window"
wait "$pid"
trap - EXIT INT TERM

# Build native functional tests with backend profiling; never bundle this binary.
CGO_ENABLED=1 go build -tags webkitgui,guittest,profile -o "$WOMPRAT_RUN_DIR/gui-acceptance" ./cmd/womprat
export WOMPRAT_PROFILE_DIR="$profiles/native"
mkdir -p "$WOMPRAT_PROFILE_DIR"
WOMPRAT_GUI_FIXTURES=1 dbus-run-session -- "$WOMPRAT_RUN_DIR/gui-acceptance" > "$WOMPRAT_RUN_DIR/fixture-gui.log" 2>&1 &
pid=$!
trap cleanup EXIT INT TERM
for _ in $(seq 1 100);do grep -q '^WOMPRAT_GUI_TEST_URL=' "$WOMPRAT_RUN_DIR/fixture-gui.log" && break;sleep .1;done
WOMPRAT_GUI_FIXTURES=1 bun tests/ux/linux-native.mjs "$WOMPRAT_RUN_DIR/fixture-gui.log"
bun -e 'const log=await Bun.file(process.argv[1]).text();const url=log.match(/^WOMPRAT_GUI_TEST_URL=(.+)$/m)[1],token=log.match(/^WOMPRAT_TOKEN=(.+)$/m)[1];const r=await fetch(new URL("quit",url),{headers:{"X-Session-Token":token}});if(!r.ok)process.exit(1)' "$WOMPRAT_RUN_DIR/fixture-gui.log"
wait "$pid"
trap - EXIT INT TERM
for sample in cpu alloc_space alloc_objects;do
 if [[ $sample == cpu ]];then go tool pprof -top -cum "$WOMPRAT_RUN_DIR/gui-acceptance" "$profiles/native/server-cpu.pprof" > "$profiles/native/cpu-top.txt";
 else go tool pprof -top -cum -sample_index="$sample" "$WOMPRAT_RUN_DIR/gui-acceptance" "$profiles/native/server-heap.pprof" > "$profiles/native/$sample-top.txt";fi
done
cp "$WOMPRAT_RUN_DIR/gui-acceptance" "$profiles/native/matching-binary"
# Root contract tests also run under the GTK-linked toolchain; copy complete
# transient inputs to the advertised analysis directory, not release assets.
CGO_ENABLED=1 bash scripts/test-profile.sh linux-gui . ./cmd/womprat -tags=webkitgui
cp -a "$(cat "$WOMPRAT_RUN_DIR/latest-evidence")" "$profiles/"
# Run the equivalent native fixture workload with C/C++ allocation capture.
# heaptrack's preload is inherited by WebKit helpers; captures are transient.
mkdir -p "$profiles/webkit-heap"
WOMPRAT_GUI_FIXTURES=1 dbus-run-session -- heaptrack -o "$profiles/webkit-heap/allocations" "$WOMPRAT_RUN_DIR/gui-acceptance" > "$WOMPRAT_RUN_DIR/heap-gui.log" 2>&1 &
pid=$!
trap cleanup EXIT INT TERM
for _ in $(seq 1 100);do grep -q '^WOMPRAT_GUI_TEST_URL=' "$WOMPRAT_RUN_DIR/heap-gui.log" && break;sleep .1;done
WOMPRAT_GUI_FIXTURES=1 bun tests/ux/linux-native.mjs "$WOMPRAT_RUN_DIR/heap-gui.log"
bun -e 'const log=await Bun.file(process.argv[1]).text();const url=log.match(/^WOMPRAT_GUI_TEST_URL=(.+)$/m)[1],token=log.match(/^WOMPRAT_TOKEN=(.+)$/m)[1];await fetch(new URL("quit",url),{headers:{"X-Session-Token":token}})' "$WOMPRAT_RUN_DIR/heap-gui.log"
wait "$pid"
trap - EXIT INT TERM
find "$profiles/webkit-heap" -name '*.gz' -print -quit | grep -q . || { echo 'Missing native allocation capture';exit 1; }
# CPU captures include the parent and inherited browser process tree.
perf record --inherit -e cpu-clock -F 99 -g -o "$profiles/webkit-cpu.data" -- env WOMPRAT_GUI_FIXTURES=1 dbus-run-session -- "$WOMPRAT_RUN_DIR/gui-acceptance" > "$WOMPRAT_RUN_DIR/cpu-gui.log" 2>&1 &
pid=$!
trap cleanup EXIT INT TERM
for _ in $(seq 1 100);do grep -q '^WOMPRAT_GUI_TEST_URL=' "$WOMPRAT_RUN_DIR/cpu-gui.log" && break;sleep .1;done
WOMPRAT_GUI_FIXTURES=1 bun tests/ux/linux-native.mjs "$WOMPRAT_RUN_DIR/cpu-gui.log"
bun -e 'const log=await Bun.file(process.argv[1]).text();const url=log.match(/^WOMPRAT_GUI_TEST_URL=(.+)$/m)[1],token=log.match(/^WOMPRAT_TOKEN=(.+)$/m)[1];await fetch(new URL("quit",url),{headers:{"X-Session-Token":token}})' "$WOMPRAT_RUN_DIR/cpu-gui.log"
wait "$pid"
trap - EXIT INT TERM
perf report --stdio -i "$profiles/webkit-cpu.data" > "$profiles/webkit-cpu.txt"
echo 'Review backend and WebKit CPU/allocation outliers before release acceptance; delete captures after analysis.'
