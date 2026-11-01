#!/usr/bin/env bash
# Release orchestration only. The Linux browser port and its acceptance tests
# must supply the hooks below; the single-webview development shim is rejected.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/paths.sh"
cd "$WOMPRAT_REPO"

if [[ $# -gt 1 || ( $# -eq 1 && $1 != --check ) ]]; then
  echo 'Usage: scripts/linux-release.sh [--check]' >&2
  exit 2
fi

required=(
  scripts/linux-appimage-build.sh
  tests/linux/browser-parity.mjs
  tests/linux/fedora-appimage.sh
)
missing=0
for hook in "${required[@]}"; do
  if [[ ! -f $hook || -L $hook ]]; then
    printf 'Missing Linux release implementation: %s\n' "$hook" >&2
    missing=1
  fi
done
if [[ $missing == 1 ]]; then
  echo 'Linux AppImage blocked: implement independent browser tabs, fail-closed tailnet routing and Fedora 44 acceptance. The single-WebView shell is not a release substitute.' >&2
  exit 1
fi
[[ ${1:-} != --check ]] || exit 0

version=${VERSION:-}
commit=${COMMIT:-}
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][A-Za-z0-9.-]+)?$ ]] || { echo 'Invalid VERSION' >&2; exit 2; }
[[ $commit =~ ^[a-f0-9]{7,40}$ ]] || { echo 'Invalid COMMIT' >&2; exit 2; }

# Build/test artifacts stay run-isolated until all acceptance checks pass.
stage=$(mktemp -d "$WOMPRAT_RUN_DIR/linux-release-XXXXXXXX")
image="$stage/Womprat-$version-x86_64.AppImage"
profiles="$stage/profiles"
mkdir -p "$profiles"
# Hook contracts and assertions are specified in docs/linux-appimage-ci.md.
VERSION="$version" COMMIT="$commit" bash scripts/linux-appimage-build.sh --output "$image"
[[ -f $image && -x $image && ! -L $image ]] || { echo 'Packaging did not produce an executable AppImage' >&2; exit 1; }
bun tests/linux/browser-parity.mjs --appimage "$image" --profiles "$profiles" --require-release
bash tests/linux/fedora-appimage.sh --appimage "$image" --profiles "$profiles" --fedora-version 44

# Only validated release assets reach the directory CI uploads/publishes.
# Profiles remain isolated for analysis, then immediate disposal; never publish them.
cp "$image" "$WOMPRAT_BUILD_DIR/dist/$(basename "$image")"
(
  cd "$WOMPRAT_BUILD_DIR/dist"
  sha256sum "$(basename "$image")" > LINUX-SHA256SUMS.txt
)
printf 'Validated AppImage: %s\nTransient profiles for analysis: %s\n' "$WOMPRAT_BUILD_DIR/dist/$(basename "$image")" "$profiles"
