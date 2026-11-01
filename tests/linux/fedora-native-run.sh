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
# Xvfb has no DRI device; use software rendering for this acceptance display.
# This does not disable sandboxing or alter network/certificate checks.
export LIBGL_ALWAYS_SOFTWARE=1 WEBKIT_DISABLE_DMABUF_RENDERER=1
mkdir -p "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$profiles"
# Diagnose host EGL and the packaged ABI boundary before browser startup.
root="$WOMPRAT_RUN_DIR/extracted/squashfs-root"
for mode in host bundled;do
 if [[ $mode == host ]];then eglinfo -B > "$WOMPRAT_RUN_DIR/egl-$mode.log" 2>&1 || true;
 else LD_LIBRARY_PATH="$root/usr/lib" EGL_LOG_LEVEL=debug LIBGL_DEBUG=verbose eglinfo -B > "$WOMPRAT_RUN_DIR/egl-$mode.log" 2>&1 || true;fi
 printf '\nEGL %s probe:\n' "$mode"
 if [[ $mode == bundled ]];then LD_LIBRARY_PATH="$root/usr/lib" ldd /usr/bin/eglinfo;fi
 grep -E 'failed|error|Error|EGL vendor|EGL version|EGL driver|undefined|symbol|not found|GLIBC|dri|dlopen' "$WOMPRAT_RUN_DIR/egl-$mode.log" | head -55 || true
 mkdir -p "$profiles/diagnostic"
 cp "$WOMPRAT_RUN_DIR/egl-$mode.log" "$profiles/diagnostic/"
done
# Exact AppImage execution uses the runtime extraction fallback. No FUSE needed.
# Deliberately set the debug env override: shipping build must ignore it.
WOMPRAT_DIRECT=1 APPIMAGE_EXTRACT_AND_RUN=1 dbus-run-session -- "$image" > "$WOMPRAT_RUN_DIR/release-gui.log" 2>&1 &
pid=$!
cleanup(){ local status=$?;if [[ $status != 0 ]];then for log in "$WOMPRAT_RUN_DIR"/{release,fixture,heap,cpu}-gui.log;do [[ ! -f $log ]]||tail -60 "$log" >&2;done;fi;kill "$pid" 2>/dev/null||true;wait "$pid" 2>/dev/null||true;}
trap cleanup EXIT INT TERM
for _ in $(seq 1 100);do grep -q '^WOMPRAT_GUI_READY=1' "$WOMPRAT_RUN_DIR/release-gui.log" && break;sleep .1;done
# Validate release renders a native window and embedded authenticated UI.
if ! xdotool search --name Womprat >/dev/null || ! grep -q '^WOMPRAT_GUI_READY=1' "$WOMPRAT_RUN_DIR/release-gui.log";then
 cat "$WOMPRAT_RUN_DIR/release-gui.log" >&2
 # Reproduce the failed exact extracted payload in the same private namespace.
 # No debug or sandbox bypass; retain only the diagnostic backtrace for analysis.
 root="$WOMPRAT_RUN_DIR/extracted/squashfs-root"
 printf 'Capturing packaged native backtrace\n' >&2
 # Keep host gdb/dbus tools on host libraries. Inject bundle environment into
 # the inferior only, so debugger dependencies cannot mask the target crash.
 timeout 45 dbus-run-session -- /usr/bin/bwrap --bind / / --dev-bind /dev /dev --tmpfs /opt --ro-bind "$root" /opt/womprat --ro-bind "$root/usr/bin/xdg-dbus-proxy" /usr/bin/xdg-dbus-proxy --chdir /opt/womprat gdb -q -batch -ex 'set environment LD_LIBRARY_PATH /opt/womprat/usr/lib' -ex 'set environment GSETTINGS_SCHEMA_DIR /opt/womprat/usr/share/glib-2.0/schemas' -ex 'set environment GDK_BACKEND x11' -ex 'set environment LIBGL_ALWAYS_SOFTWARE 1' -ex 'set environment WEBKIT_DISABLE_DMABUF_RENDERER 1' -ex 'handle SIGUSR1 nostop noprint pass' -ex 'handle SIGUSR2 nostop noprint pass' -ex run -ex 'bt 30' -ex 'info sharedlibrary' --args /opt/womprat/usr/bin/womprat > "$WOMPRAT_RUN_DIR/native-crash.txt" 2>&1 || true
 mkdir -p "$profiles/diagnostic"
 cp "$WOMPRAT_RUN_DIR/native-crash.txt" "$profiles/diagnostic/"
 head -100 "$WOMPRAT_RUN_DIR/native-crash.txt" >&2
 tail -40 "$WOMPRAT_RUN_DIR/native-crash.txt" >&2
 exit 1
fi
bun tests/linux/release-smoke.mjs "$WOMPRAT_RUN_DIR/release-gui.log"
# Send GTK's close protocol, not XDestroyWindow/SIGKILL. Xvfb has no WM.
window=$(xdotool search --name Womprat | tail -1)
cc tests/linux/close-window.c -lX11 -o "$WOMPRAT_RUN_DIR/close-window"
"$WOMPRAT_RUN_DIR/close-window" "$window"
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
