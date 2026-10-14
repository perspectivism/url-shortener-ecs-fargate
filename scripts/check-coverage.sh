#!/usr/bin/env bash
# Enforces the unit-test coverage gate on internal/... (SPEC §16).
# Usage: scripts/check-coverage.sh <coverprofile> <min-percent>
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
profile="$1"
min="$2"
filtered="$(mktemp)"
trap 'rm -f "$filtered"' EXIT

head -1 "$profile" >"$filtered"
grep -E '^[^:]+/internal/' "$profile" >>"$filtered" || true
if [[ "$(wc -l <"$filtered")" -le 1 ]]; then
  echo "coverage: no internal/ packages in $profile" >&2
  exit 1
fi

total="$(go tool cover -func="$filtered" | awk '/^total:/ { sub(/%/, "", $3); print $3 }')"
echo "coverage (internal/...): ${total}% (minimum ${min}%)"
awk -v t="$total" -v m="$min" 'BEGIN { exit !(t + 0 >= m + 0) }' || {
  echo "coverage gate failed: ${total}% < ${min}%" >&2
  exit 1
}
