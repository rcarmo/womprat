#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/paths.sh"
label=${1:?}; shift
[[ $label =~ ^[a-zA-Z0-9_.-]+$ ]] || exit 2
# Bun's preload afterAll is file-scoped: one process per file ensures complete
# capture instead of stopping after the first file in a multi-file invocation.
if [[ ${1:-} == test && $# -gt 2 ]]; then
  shift
  status=0
  for file in "$@"; do
    [[ $file == *.test.mjs ]] || { echo 'Pass one test file when using flags' >&2; exit 2; }
    bash "$WOMPRAT_REPO/scripts/bun-profile.sh" "$label-${file##*/}" test "$file" || status=1
  done
  exit "$status"
fi
mkdir -p "$WOMPRAT_EVIDENCE_ROOT/tests"
export WOMPRAT_PROFILE_DIR
WOMPRAT_PROFILE_DIR=$(mktemp -d "$WOMPRAT_EVIDENCE_ROOT/tests/$label-$(date -u +%Y%m%dT%H%M%S)-XXXXXX")
{ git -C "$WOMPRAT_REPO" rev-parse HEAD; bun --version; printf 'Bun JSC sampling profiler (1000us interval); full V8-format heap snapshot\n'; printf '%q ' "$@"; echo; } > "$WOMPRAT_PROFILE_DIR/metadata.txt"
ulimit -c 0 # never let a profiling runtime crash write a host core dump
status=0
verb=$1; shift
export WOMPRAT_PROFILE_TEST=0
[[ $verb != test ]] || export WOMPRAT_PROFILE_TEST=1
bun "$verb" --preload "$WOMPRAT_REPO/scripts/profile-preload.mjs" "$@" > "$WOMPRAT_PROFILE_DIR/test.log" 2>&1 || status=$?
cat "$WOMPRAT_PROFILE_DIR/test.log"
# Missing captures fail closed, including abrupt exit.
if [[ -n ${WOMPRAT_BIN:-} ]]; then
  cp "$WOMPRAT_BIN" "$WOMPRAT_PROFILE_DIR/server.bin"
  if [[ -s $WOMPRAT_PROFILE_DIR/server-cpu.pprof && -s $WOMPRAT_PROFILE_DIR/server-heap.pprof ]]; then
    go tool pprof -top -cum "$WOMPRAT_PROFILE_DIR/server.bin" "$WOMPRAT_PROFILE_DIR/server-cpu.pprof" > "$WOMPRAT_PROFILE_DIR/server-cpu-top.txt"
    for sample in alloc_space alloc_objects; do
      go tool pprof -top -cum -sample_index="$sample" "$WOMPRAT_PROFILE_DIR/server.bin" "$WOMPRAT_PROFILE_DIR/server-heap.pprof" > "$WOMPRAT_PROFILE_DIR/server-$sample-top.txt"
    done
  else
    echo 'MISSING Go UX server profiles' >&2; status=1
  fi
fi
test -s "$WOMPRAT_PROFILE_DIR/bun-cpu.json" || { echo 'MISSING Bun CPU profile' >&2; status=1; }
find "$WOMPRAT_PROFILE_DIR" -type f -name '*.heapsnapshot' | grep -q . || { echo 'MISSING Bun heap profile' >&2; status=1; }
echo "Evidence: $WOMPRAT_PROFILE_DIR (review CPU and heap before acceptance)"
exit "$status"
