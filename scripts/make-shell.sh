#!/usr/bin/env bash
# GNU make invokes this wrapper for each recipe, including nested make calls.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/paths.sh"
exec bash "$@"
