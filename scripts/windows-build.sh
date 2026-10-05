#!/usr/bin/env bash
# Go requires .syso next to package sources; use an isolated source copy.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/paths.sh"
arch=${1:?}; output=${2:?}; flags=${3:-}
[[ $arch == amd64 || $arch == arm64 ]] || exit 2
stage=$(mktemp -d "$WOMPRAT_RUN_DIR/windows-$arch-XXXXXX")
tar -C "$WOMPRAT_REPO" --exclude=.git --exclude=evidence --exclude=dist --exclude=.tmp --exclude=node_modules --exclude='*.syso' -cf - . | tar -C "$stage" -xf -
cp "$WOMPRAT_BUILD_DIR/resources/rsrc_windows_$arch.syso" "$stage/cmd/womprat/"
cd "$stage"
GOOS=windows GOARCH="$arch" "${GO:-go}" build -ldflags="$flags" -o "$output" ./cmd/womprat
