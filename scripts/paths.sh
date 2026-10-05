#!/usr/bin/env bash
# Source this before direct build/test/install commands. No fallback to host caches.
set -euo pipefail
WOMPRAT_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
# Resolve once, before replacing the caller's TMPDIR; children inherit the root.
source "$WOMPRAT_REPO/scripts/project-tmp.sh"
WOMPRAT_REPO=$(project_normalise "$WOMPRAT_REPO")
export PROJECT_TMP_ROOT
PROJECT_TMP_ROOT=$(project_tmp_resolve womprat) || { return 1 2>/dev/null || exit 1; }
project_tmp_init "$PROJECT_TMP_ROOT"
export WOMPRAT_TMP_ROOT="$PROJECT_TMP_ROOT"
# Refuse symlinks below the project root, including existing ancestors of new paths.
womprat_owned_dir() {
  local dir=$1 part rel current=$WOMPRAT_TMP_ROOT
  [[ $dir == "$current" || $dir == "$current/"* ]] || { echo "Outside Womprat root: $dir" >&2; return 1; }
  [[ $dir != *'/../'* && $dir != */.. && $dir != *'/./'* ]] || return 1
  project_path_usable "$current" || { echo "Unsafe root: $current" >&2; return 1; }
  mkdir -p "$current"
  [[ -d $current && -O $current ]] || return 1
  rel=${dir#"$current"}; rel=${rel#/}
  local IFS=/
  for part in $rel; do
    current="$current/$part"
    [[ ! -L $current ]] || { echo "Symlink directory: $current" >&2; return 1; }
    mkdir -p "$current"
    [[ -d $current && -O $current ]] || return 1
  done
}
export WOMPRAT_RUN_DIR="${WOMPRAT_RUN_DIR:-$WOMPRAT_TMP_ROOT/runs/direct/$(date -u +%Y%m%dT%H%M%S)-$$}"
[[ $WOMPRAT_RUN_DIR == "$WOMPRAT_TMP_ROOT/runs/"*/* ]] || { echo 'Run directory must be runs/<purpose>/<run-id>' >&2; return 1 2>/dev/null || exit 1; }
womprat_owned_dir "$WOMPRAT_RUN_DIR"
export TMPDIR="$WOMPRAT_RUN_DIR/tmp" TMP="$WOMPRAT_RUN_DIR/tmp" TEMP="$WOMPRAT_RUN_DIR/tmp"
export GOCACHE="$WOMPRAT_TMP_ROOT/cache/go/build" GOMODCACHE="$WOMPRAT_TMP_ROOT/cache/go/mod" GOPATH="$WOMPRAT_TMP_ROOT/cache/go/path"
export GOBIN="$WOMPRAT_TMP_ROOT/build/tools" GOTMPDIR="$WOMPRAT_RUN_DIR/go"
export BUN_INSTALL_CACHE_DIR="$WOMPRAT_TMP_ROOT/cache/bun" npm_config_cache="$WOMPRAT_TMP_ROOT/cache/npm"
export PLAYWRIGHT_BROWSERS_PATH="$WOMPRAT_TMP_ROOT/cache/playwright" XDG_CACHE_HOME="$WOMPRAT_TMP_ROOT/cache/xdg"
export PYTHONPYCACHEPREFIX="$WOMPRAT_TMP_ROOT/cache/python" PIP_CACHE_DIR="$WOMPRAT_TMP_ROOT/cache/pip" UV_CACHE_DIR="$WOMPRAT_TMP_ROOT/cache/uv"
export WINEPREFIX="$WOMPRAT_TMP_ROOT/cache/wine" GOLANGCI_LINT_CACHE="$WOMPRAT_TMP_ROOT/cache/golangci-lint"
export TINYGOCACHE="$WOMPRAT_TMP_ROOT/cache/tinygo"
export WOMPRAT_BUILD_DIR="$WOMPRAT_TMP_ROOT/build"
export WOMPRAT_EVIDENCE_ROOT="$WOMPRAT_REPO/evidence"
for dir in "$WOMPRAT_BUILD_DIR/dist" "$WOMPRAT_BUILD_DIR/resources" "$WOMPRAT_RUN_DIR/generated" "$TMPDIR" "$GOTMPDIR" "$GOCACHE" "$GOMODCACHE" "$GOPATH" "$GOBIN" "$BUN_INSTALL_CACHE_DIR" "$npm_config_cache" "$PLAYWRIGHT_BROWSERS_PATH" "$XDG_CACHE_HOME" "$PYTHONPYCACHEPREFIX" "$PIP_CACHE_DIR" "$UV_CACHE_DIR" "$WINEPREFIX" "$GOLANGCI_LINT_CACHE" "$TINYGOCACHE"; do
  womprat_owned_dir "$dir"
done
# Evidence has separate retention but must never follow a redirected root.
for dir in "$WOMPRAT_EVIDENCE_ROOT" "$WOMPRAT_EVIDENCE_ROOT/tests"; do
  [[ ! -L $dir ]] || { echo "Symlink evidence directory: $dir" >&2; return 1 2>/dev/null || exit 1; }
  mkdir -p "$dir"
  [[ -O $dir && -w $dir ]] || { echo "Unowned evidence directory: $dir" >&2; return 1 2>/dev/null || exit 1; }
done
export PATH="$GOBIN:$PATH"
export WOMPRAT_PATHS_READY=1
