#!/usr/bin/env bash
# Every SPEC §13 EMF metric must be (1) a telemetry constant, (2) emitted by
# non-test code outside internal/telemetry, and (3) on the dashboard.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

metrics=(RedirectLatencyMs CacheHit CacheMiss NegativeCacheHit CacheErrors ClickEventsEnqueued
  ClickEventsDropped LinksCreated URLScanFlagged URLScanUnavailable URLScanSkipped LinksDisabled
  WorkerEventsProcessed WorkerDuplicateEvents KeyBlocksAllocated CacheInvalidationFailed RateLimited)

fail=0
for m in "${metrics[@]}"; do
  if ! grep -Eq "^\s*$m\s*=\s*\"$m\"" internal/telemetry/telemetry.go; then
    echo "FAIL: $m is not a constant in internal/telemetry" >&2; fail=1; continue
  fi
  if ! grep -rlw --include='*.go' --exclude='*_test.go' "telemetry\.$m" internal cmd | grep -qv '^internal/telemetry/'; then
    echo "FAIL: $m is never emitted" >&2; fail=1
  fi
  if ! grep -qw "$m" infra/modules/observability/dashboard.tf; then
    echo "FAIL: $m is not on the dashboard" >&2; fail=1
  fi
done
[[ $fail -eq 0 ]] || exit 1
echo "metrics OK: ${#metrics[@]} SPEC §13 metrics defined, emitted and on the dashboard"
