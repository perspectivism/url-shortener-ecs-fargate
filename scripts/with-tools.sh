#!/usr/bin/env bash
# Runs a command with the pinned toolchain (./.tools/bin) first on PATH.
# Used by pre-commit hooks so they use exactly the versions in .tool-versions.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
export GOTOOLCHAIN=local
export TFLINT_PLUGIN_DIR="$ROOT/.tools/tflint-plugins"
exec "$@"
