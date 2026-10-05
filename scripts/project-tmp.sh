#!/usr/bin/env bash
# Portable project-owned scratch resolver. Source to reuse functions in tests.
# Resolve before exporting TMPDIR to a child run directory. Never removes files.
set -euo pipefail
# Capture the incoming value once; child tools may later replace TMPDIR.
if [[ -z "${PROJECT_ORIGINAL_TMPDIR+x}" ]]; then
  export PROJECT_ORIGINAL_TMPDIR="${TMPDIR:-}"
fi
# Vendored resolver; keep policy in sync with the documented workspace amendment.
# Native Windows Go needs drive paths, while Git Bash also accepts POSIX paths.
project_absolute() { case "$1" in /*) return 0;; [A-Za-z]:/*|[A-Za-z]:\\*) command -v cygpath >/dev/null;; *) return 1;; esac; }
project_normalise() {
  project_absolute "$1" || return 1
  local lexical=${1//\\//}
  case "/${lexical#/}/" in */../*|*/./*) return 1;; esac
  if command -v cygpath >/dev/null 2>&1; then cygpath -am "$1"; else printf '%s\n' "$1"; fi
}
# Capture Windows' platform temp before TMP/TEMP are redirected into run scratch.
if [[ -z ${PROJECT_SYSTEM_TEMP+x} ]]; then
  if command -v cygpath >/dev/null 2>&1; then
    export PROJECT_SYSTEM_TEMP="$(project_normalise "${TEMP:-${TMP:-/tmp}}")"
  elif [[ $(uname -s) == Darwin ]]; then
    # macOS /tmp and /var are symlinks; use the physical per-user platform temp.
    export PROJECT_SYSTEM_TEMP="$(cd "$(getconf DARWIN_USER_TEMP_DIR)" && pwd -P)"
  else
    export PROJECT_SYSTEM_TEMP=/tmp
  fi
fi
project_is_ci() {
  case "${CI:-}" in ''|0|false|FALSE) ;; *) return 0;; esac
  case "${GITHUB_ACTIONS:-}:${GITLAB_CI:-}:${TF_BUILD:-}:${CIRCLECI:-}" in
    *true*|*True*|*TRUE*) return 0;;
  esac
  return 1
}

project_name_valid() {
  case "$1" in ''|*[!A-Za-z0-9._-]*|.*|-*) return 1;; esac
}
project_path_usable() {
  local path="$1" parent ancestor
  project_absolute "$path" || return 1
  case "/${path#/}/" in */../*|*/./*) return 1;; esac
  [[ ! -L "$path" ]] || return 1
  ancestor="${path%/*}"
  while [[ -n "$ancestor" && "$ancestor" != / && ! "$ancestor" =~ ^[A-Za-z]:$ ]]; do
    # /workspace is the operator-provided mount alias on this host.
    [[ ! -L "$ancestor" || "$ancestor" == /workspace ]] || return 1
    ancestor="${ancestor%/*}"
  done
  if [[ -e "$path" ]]; then
    [[ -d "$path" && -O "$path" && -w "$path" && -x "$path" ]] || return 1
  fi
  parent="$path"
  while [[ ! -e "$parent" && ! -L "$parent" ]]; do parent="${parent%/*}"; [[ -n "$parent" ]] || parent=/; done
  [[ -d "$parent" && -w "$parent" && -x "$parent" ]] || return 1
}
project_tmp_resolve() {
  local project="$1" workspace_base="${2:-/workspace/tmp}" base candidate explicit_base_root=''
  project_name_valid "$project" || { echo 'Invalid canonical project name' >&2; return 1; }
  if [[ -n "${PROJECT_TMP_BASE+x}" ]]; then
    [[ -n "$PROJECT_TMP_BASE" ]] || { echo 'PROJECT_TMP_BASE must not be empty' >&2; return 1; }
    base=$(project_normalise "$PROJECT_TMP_BASE") || { echo 'PROJECT_TMP_BASE must be absolute' >&2; return 1; }
    explicit_base_root="${base%/}/$project"
    project_path_usable "$explicit_base_root" || { echo 'PROJECT_TMP_BASE must be a usable absolute base' >&2; return 1; }
  fi
  if [[ -n "${PROJECT_TMP_ROOT+x}" ]]; then
    candidate=$(project_normalise "${PROJECT_TMP_ROOT%/}") || { echo 'PROJECT_TMP_ROOT must be absolute' >&2; return 1; }
    [[ "${candidate##*/}" == "$project" ]] && project_path_usable "$candidate" || {
      echo 'PROJECT_TMP_ROOT must be a usable absolute project-named directory, not a symlink' >&2; return 1;
    }
    [[ -z "$explicit_base_root" || "$candidate" == "$explicit_base_root" ]] || { echo 'Conflicting PROJECT_TMP_BASE and PROJECT_TMP_ROOT' >&2; return 1; }
    printf '%s\n' "$candidate"; return
  fi
  if [[ -n "$explicit_base_root" ]]; then printf '%s\n' "$explicit_base_root"; return; fi
  # CI never selects a host workspace mount; a local host uses workspace then system temp.
  # workspace_base is injectable only through the sourced API for isolated validation.
  local bases=()
  if project_is_ci; then
    bases=("${RUNNER_TEMP:-}" "$PROJECT_ORIGINAL_TMPDIR" "$PROJECT_SYSTEM_TEMP")
  else
    if [[ ! -d "$workspace_base" && ! -d "${workspace_base%/*}" ]]; then workspace_base=''; fi
    bases=("$workspace_base" "$PROJECT_SYSTEM_TEMP")
  fi
  for base in "${bases[@]}"; do
    [[ -n "$base" ]] || continue
    base=$(project_normalise "$base") || continue
    candidate="${base%/}/$project"
    if project_path_usable "$candidate"; then printf '%s\n' "$candidate"; return; fi
  done
  echo 'No writable project-owned temporary root available' >&2; return 1
}
project_tmp_init() {
  local root="$1" path
  for path in "$root" "$root/cache" "$root/build" "$root/tests" "$root/logs" "$root/runs"; do
    project_path_usable "$path" || { echo "Unsafe scratch path: $path" >&2; return 1; }
  done
  mkdir -p "$root/cache" "$root/build" "$root/tests" "$root/logs" "$root/runs"
}
project_tmp_main() {
  local action="${1:-paths}" root
  case "$action" in paths|init) ;; *) echo 'Usage: PROJECT=name project-tmp.sh paths|init' >&2; return 1;; esac
  root="$(project_tmp_resolve "${PROJECT:-}")" || return
  if [[ "$action" == init ]]; then project_tmp_init "$root" || return; fi
  printf '%s\n' "PROJECT_TMP_ROOT=$root" "CACHE_ROOT=$root/cache" "BUILD_ROOT=$root/build" "TEST_ROOT=$root/tests" "LOG_ROOT=$root/logs" "RUN_ROOT=$root/runs"
}
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then project_tmp_main "$@"; fi
