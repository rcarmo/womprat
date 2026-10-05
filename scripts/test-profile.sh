#!/usr/bin/env bash
# One package per invocation: Go does not support multi-package -cpuprofile.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/paths.sh"
label=${1:?usage: test-profile.sh label module-dir packages [go test flags...]}; shift
module=${1:?}; shift
packages=${1:?}; shift
[[ $label =~ ^[a-zA-Z0-9_-]+$ ]] || exit 2
mkdir -p "$WOMPRAT_EVIDENCE_ROOT/tests"
evidence=$(mktemp -d "$WOMPRAT_EVIDENCE_ROOT/tests/$label-$(date -u +%Y%m%dT%H%M%S)-XXXXXX")
printf '%s\n' "$evidence" > "$WOMPRAT_RUN_DIR/latest-evidence"
cd "$module"
{ git -C "$WOMPRAT_REPO" rev-parse HEAD; git -C "$WOMPRAT_REPO" diff --stat; go version; go env GOOS GOARCH GOCACHE GOMODCACHE GOTMPDIR; printf 'CPU=100Hz; memory=1 byte; package pattern=%s\n' "$packages"; printf 'go test %q ' "$@"; echo; } > "$evidence/metadata.txt"
# Respect build tags during package discovery. No test execution occurs here.
list_flags=()
for arg in "$@"; do [[ $arg != -tags=* ]] || list_flags+=("$arg"); done
if ! go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' "${list_flags[@]}" "$packages" > "$evidence/packages.txt" 2> "$evidence/discovery.log"; then
  echo "Package discovery failed; no profiles captured: $evidence" >&2; exit 1
fi
status=0
if ! grep -q '[^[:space:]]' "$evidence/packages.txt"; then
  echo 'No test packages discovered; no profiles captured.' | tee "$evidence/capture-warnings.txt"
  exit 1
fi
while IFS= read -r package; do
  [[ -n $package ]] || continue
  dir="$evidence/${package//\//_}"
  mkdir "$dir"
  binary="$dir/tests.test"
  [[ $(go env GOOS) != windows ]] || binary="$binary.exe"
  command=(go test -count=1 -timeout=180s -o "$binary" -cpuprofile="$dir/cpu.pprof" -memprofile="$dir/heap.pprof" -memprofilerate=1 "$@" "$package")
  printf '%q ' "${command[@]}" > "$dir/command.txt"; echo >> "$dir/command.txt"
  "${command[@]}" > "$dir/test.log" 2>&1 || status=1
  cat "$dir/test.log"
  if [[ -s $dir/cpu.pprof && -s $dir/heap.pprof && -s $binary ]]; then
    go tool pprof -top -cum "$binary" "$dir/cpu.pprof" > "$dir/cpu-top.txt" 2>&1 || status=1
    for sample in alloc_space alloc_objects; do
      go tool pprof -top -cum -sample_index="$sample" "$binary" "$dir/heap.pprof" > "$dir/$sample-top.txt" 2>&1 || status=1
    done
    cat "$dir/cpu-top.txt" "$dir/alloc_space-top.txt" "$dir/alloc_objects-top.txt"
    if grep -q 'Total samples = 0' "$dir/cpu-top.txt"; then
      echo 'WARNING: empty CPU samples; obtain representative workload before performance acceptance.' | tee -a "$evidence/capture-warnings.txt"
    fi
  else
    echo "MISSING profile or binary: $package (build failure, no tests or abrupt exit)." | tee -a "$evidence/capture-warnings.txt"
    status=1
  fi
done < "$evidence/packages.txt"
echo "Evidence: $evidence; review cumulative CPU, alloc_space and alloc_objects before acceptance."
exit "$status"
