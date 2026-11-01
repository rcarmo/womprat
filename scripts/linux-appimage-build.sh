#!/usr/bin/env bash
# Build the native Linux GUI and bundle its WebKit runtime. Docker supplies a
# stable glibc baseline; source/build/cache paths remain project-owned.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/paths.sh"
cd "$WOMPRAT_REPO"
[[ $# == 2 && $1 == --output ]] || { echo 'Usage: linux-appimage-build.sh --output <run-owned AppImage>' >&2; exit 2; }
output=$2
case "$output" in "$WOMPRAT_RUN_DIR"/*/*.AppImage) ;; *) echo 'Output must be below the active project run' >&2; exit 2;; esac
[[ $output != *'/../'* && ! -e $output ]] || { echo 'Unsafe/existing output' >&2; exit 2; }
[[ ${VERSION:-} =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][A-Za-z0-9.-]+)?$ && ${COMMIT:-} =~ ^[a-f0-9]{7,40}$ ]] || { echo 'Invalid version/commit' >&2; exit 2; }
stage=$(mktemp -d "$WOMPRAT_RUN_DIR/appimage-build-XXXXXXXX")
mkdir -p "$stage/tmp" "$stage/source" "$stage/tools" "$stage/home"
chmod 1777 "$stage/tmp"
# Exclude legacy outputs, secret state, git and concise evidence from source staging.
tar --exclude=.git --exclude=evidence --exclude=dist --exclude=.tmp --exclude=coverage.out --exclude=./womprat --exclude='*.exe' --exclude='*.syso' --exclude=node_modules -cf - . | tar -xf - -C "$stage/source"
cache="$WOMPRAT_TMP_ROOT/cache/appimage"
womprat_owned_dir "$cache"
fetch() {
  local url=$1 name=$2 hash=$3
  if [[ ! -f $cache/$name ]]; then curl -fLSs --retry 2 "$url" -o "$cache/$name.part"; mv "$cache/$name.part" "$cache/$name"; fi
  printf '%s  %s\n' "$hash" "$cache/$name" | sha256sum --check --status || { echo "Tool integrity mismatch: $name" >&2; exit 1; }
  cp "$cache/$name" "$stage/tools/$name"
}
fetch https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage linuxdeploy.AppImage d900b99faf8656aa613038dafbd2cc23ca7f90794a9a00c9546f0ab572af94dd
fetch https://github.com/AppImage/type2-runtime/releases/download/continuous/runtime-x86_64 runtime-x86_64 617aa77314d57c8b8e4fae9c3f0b35af7b30d5529cb69e98f64f92510ed98a46
fetch https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk.sh linuxdeploy-plugin-gtk.sh b0f4cbc684a0103a9651f0955b635eaea0096b3a66c0f5a2c2aa337960375171
chmod +x "$stage/tools/"*
goroot=$(go env GOROOT)
bun=$(command -v bun)
name="womprat-appimage-$(basename "$stage")"
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM
docker run --detach --name "$name" \
  --mount "type=bind,src=$WOMPRAT_TMP_ROOT,dst=$WOMPRAT_TMP_ROOT" \
  --mount "type=bind,src=$stage/tmp,dst=/tmp" \
  --mount "type=bind,src=$bun,dst=/usr/local/bin/bun,readonly" \
  --mount "type=bind,src=$goroot,dst=$goroot,readonly" \
  --env PROJECT_TMP_ROOT="$WOMPRAT_TMP_ROOT" --env WOMPRAT_RUN_DIR="$stage" \
  --env HOME="$stage/home" --env PATH="$goroot/bin:/usr/local/bin:/usr/bin:/bin" \
  --env VERSION --env COMMIT \
  debian:bookworm-slim sleep infinity >/dev/null
docker exec "$name" bash -c 'apt-get update -qq; apt-get install -y --no-install-recommends build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev ca-certificates curl squashfs-tools git librsvg2-common file' > "$stage/install.log" 2>&1
# Docker/apt can change the owner of the bind-mounted /tmp. Restore caller
# ownership only after apt has finished, before entering the guarded paths.
docker exec "$name" chown "$(id -u):$(id -g)" "$stage/tmp"
# Runtime dependencies installed by apt are container-owned; project state is
# written as the caller so path isolation/ownership guards remain intact.
docker exec --user "$(id -u):$(id -g)" "$name" bash "$stage/source/scripts/linux-appimage-container.sh" "$stage" "$output"
