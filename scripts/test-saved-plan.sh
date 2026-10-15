#!/usr/bin/env bash
# Self-test for scripts/saved-plan.sh: a plan survives the hand-over byte for
# byte, and a missing, altered or substituted plan is refused (ADR-0028).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
sp="$here/saved-plan.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export GITHUB_RUN_ID=42 S3_URL_PREFIX="file://$work/bucket" PLAN_DIR="$work/plan"
mkdir -p "$PLAN_DIR"
fail=0
ok() { echo "ok   $1"; }
no() { echo "FAIL $1" >&2; fail=1; }
check() { # description, command...
  local d="$1"; shift
  if "$@"; then ok "$d"; else no "$d"; fi
}

printf 'approved plan\n' >"$PLAN_DIR/tfplan"
echo "create=1" >"$PLAN_DIR/plan-summary.txt"
sha="$("$sp" put dev)"
check "put prints the plan's SHA-256" test "$sha" == "$(sha256sum "$PLAN_DIR/tfplan" | cut -d' ' -f1)"
check "stored under plans/<env>/<run>/<sha>/" test -f "$work/bucket/plans/dev/42/$sha/tfplan"

rm -f "$PLAN_DIR/tfplan"
"$sp" get dev "$sha" >/dev/null
check "get restores the approved plan" test "$(cat "$PLAN_DIR/tfplan")" == "approved plan"

printf 'something else\n' >"$work/bucket/plans/dev/42/$sha/tfplan"
if "$sp" get dev "$sha" 2>/dev/null; then no "an altered plan was accepted"; else ok "an altered plan is refused"; fi
check "a refused plan is not left behind" test ! -f "$PLAN_DIR/tfplan"

if "$sp" get dev "$(printf 'x' | sha256sum | cut -d' ' -f1)" 2>/dev/null; then no "a missing plan was accepted"; else ok "a missing plan is refused"; fi
if "$sp" get prod "$sha" 2>/dev/null; then no "another environment's plan was accepted"; else ok "plans are per environment"; fi
if "$sp" get dev "not-a-sha" 2>/dev/null; then no "malformed sha accepted"; else ok "a malformed SHA-256 is refused"; fi

"$sp" rm dev "$sha"
check "rm deletes the applied plan" test ! -d "$work/bucket/plans/dev/42/$sha"
exit "$fail"
