#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/../../scripts/paths.sh"
cd "$WOMPRAT_REPO"
image='' profiles='' fedora='' preflight=0
while [[ $# -gt 0 ]]; do
  case $1 in
    --appimage) image=$2;shift 2;;
    --profiles) profiles=$2;shift 2;;
    --fedora-version) fedora=$2;shift 2;;
    --preflight-only) preflight=1;shift;;
    *) echo "Unknown argument: $1" >&2;exit 2;;
  esac
done
[[ $fedora == 44 && $image == "$WOMPRAT_RUN_DIR"/*/*.AppImage && -f $image && ! -L $image && $profiles == "$WOMPRAT_RUN_DIR"/*/profiles ]] || { echo 'Invalid run-owned acceptance arguments' >&2;exit 2; }
[[ $image != *'/../'* && $profiles != *'/../'* ]] || exit 2
stage=$(mktemp -d "$WOMPRAT_RUN_DIR/fedora-acceptance-XXXXXXXX")
mkdir -p "$stage/tmp" "$stage/home" "$stage/source" "$profiles"
chmod 1777 "$stage/tmp"
tar --exclude=.git --exclude=evidence --exclude=dist --exclude=.tmp --exclude=coverage.out --exclude=./womprat --exclude='*.exe' --exclude='*.syso' --exclude=node_modules -cf - . | tar -xf - -C "$stage/source"
name="womprat-fedora-$(basename "$stage")"
step='container setup'
cleanup(){
  local status=$?
  if [[ $status != 0 ]];then
    printf 'Fedora acceptance failed at %s (exit %s)\n' "$step" "$status" >&2
    for log in "$stage"/{install,build-deps,xvfb,release-gui,fixture-gui,heap-gui,cpu-gui}.log "$stage/native-crash.txt";do
      if [[ -f $log ]];then printf '\n=== %s ===\n' "$(basename "$log")" >&2;tail -60 "$log" >&2;fi
    done
  fi
  docker rm -f "$name" >/dev/null 2>&1||true
}
trap cleanup EXIT INT TERM
# User namespaces inside WebKit's bubblewrap sandbox need namespace capability
# in the outer disposable container. The GUI itself runs as an ordinary user.
docker run --detach --name "$name" --privileged \
 --mount "type=bind,src=$WOMPRAT_TMP_ROOT,dst=$WOMPRAT_TMP_ROOT" \
 --mount "type=bind,src=$stage/tmp,dst=/tmp" \
 --mount "type=bind,src=$(command -v bun),dst=/usr/local/bin/bun,readonly" \
 --mount "type=bind,src=$(go env GOROOT),dst=$(go env GOROOT),readonly" \
 --env PROJECT_TMP_ROOT="$WOMPRAT_TMP_ROOT" --env WOMPRAT_RUN_DIR="$stage" \
 --env HOME="$stage/home" --env WOMPRAT_SOURCE_COMMIT="$(git rev-parse HEAD)" --env PATH="$(go env GOROOT)/bin:/usr/local/bin:/usr/bin:/bin" \
 registry.fedoraproject.org/fedora:44 sleep infinity >/dev/null
step='Fedora runtime dependency installation'
docker exec "$name" bash -c 'dnf install -y --setopt=cachedir="$WOMPRAT_RUN_DIR/dnf-cache" --setopt=logdir="$WOMPRAT_RUN_DIR/dnf-logs" xorg-x11-server-Xvfb xdotool dbus-x11 bubblewrap xdg-dbus-proxy libX11 libXext libXrender libXrandr libXcursor libXi libXfixes libXinerama libstdc++ libgcc fontconfig freetype dejavu-sans-fonts mesa-libGL mesa-libEGL libwayland-server libwayland-client egl-utils procps-ng curl' > "$stage/install.log" 2>&1
step='normal user setup'
docker exec "$name" bash -c 'useradd -u "$1" -M womprat-ci;chown "$1:$2" "$WOMPRAT_RUN_DIR/tmp"' sh "$(id -u)" "$(id -g)"
step='exact package dependency audit'
docker exec --user "$(id -u):$(id -g)" "$name" bash -c '
 set -euo pipefail
 mkdir -p "$WOMPRAT_RUN_DIR/extracted";cd "$WOMPRAT_RUN_DIR/extracted"
 "$1" --appimage-extract > "$WOMPRAT_RUN_DIR/extraction.log"
 export LD_LIBRARY_PATH="$PWD/squashfs-root/usr/lib"
 missing=0
 while IFS= read -r -d "" file;do
   result=$(ldd "$file" 2>/dev/null || true)
   if [[ $result == *"not found"* ]];then printf "%s\n%s\n" "$file" "$result";missing=1;fi
 done < <(find squashfs-root/usr -type f \( -name "*.so*" -o -name womprat -o -name WebKitWebProcess -o -name WebKitNetworkProcess -o -name WebKitGPUProcess -o -name gst-plugin-scanner \) -print0)
 [[ $missing == 0 ]] || { echo "Unbundled Fedora dependencies" >&2;exit 1; }
 /usr/bin/env -u LD_LIBRARY_PATH go version -m squashfs-root/usr/bin/womprat > "$WOMPRAT_RUN_DIR/release-build-settings.txt"
 grep -q -- "-tags=webkitgui" "$WOMPRAT_RUN_DIR/release-build-settings.txt"
 ! grep -qE "guittest|main.debugBuild=1" "$WOMPRAT_RUN_DIR/release-build-settings.txt"
 echo "PASS Fedora 44 dependency and release-build preflight (no host GTK/WebKit)"
' sh "$image" > "$stage/preflight.log" 2>&1 || { cat "$stage/preflight.log";exit 1; }
cat "$stage/preflight.log"
[[ $preflight == 0 ]] || exit 0
# Native release startup must work with the sandbox enabled for a normal user.
# A host that forbids UID namespace maps is a real blocker, never a green skip.
docker exec --user "$(id -u):$(id -g)" "$name" bwrap --ro-bind / / /bin/true || { echo 'Fedora normal-user sandbox namespace creation failed; acceptance blocked' >&2;exit 1; }
step='native build/profiling dependency installation'
# Install build dependencies only after the exact AppImage's runtime audit,
# so they cannot hide packaging omissions.
docker exec "$name" bash -c 'dnf install -y --setopt=cachedir="$WOMPRAT_RUN_DIR/dnf-cache" --setopt=logdir="$WOMPRAT_RUN_DIR/dnf-logs" gcc gcc-c++ pkgconf-pkg-config gtk3-devel webkit2gtk4.1-devel git perf libcap heaptrack hostname gdb' > "$stage/build-deps.log" 2>&1
# The host denies perf_event_open to ordinary users (paranoid=4). Give only
# the collector CAP_PERFMON; exec of the unprivileged application drops it.
# Do not change host sysctls or run the GUI as root to obtain a profile.
step='CPU collector capability setup'
docker exec "$name" setcap cap_perfmon=ep /usr/bin/perf
step='Xvfb startup'
# Package installation may reset /tmp ownership. Restore the owned mount only
# after that job is idle, and start the display under the acceptance UID.
docker exec "$name" chown "$(id -u):$(id -g)" "$stage/tmp"
docker exec --user "$(id -u):$(id -g)" "$name" bash -c 'Xvfb :91 -screen 0 1200x800x24 -nolisten tcp > "$WOMPRAT_RUN_DIR/xvfb.log" 2>&1 & sleep 1'
step='native release/fixture acceptance'
docker exec --user "$(id -u):$(id -g)" --env DISPLAY=:91 "$name" bash "$stage/source/tests/linux/fedora-native-run.sh" "$image" "$profiles"
