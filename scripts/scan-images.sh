#!/usr/bin/env bash
# Scans built images (SPEC §18 M3):
#  1. Trivy vulnerabilities: fail on fixable HIGH/CRITICAL.
#  2. Trivy secret scanner over every layer.
#  3. Build history review: only the expected build args (SERVICE, VERSION,
#     TARGET*) may appear, and nothing secret-looking.
# Usage: scripts/scan-images.sh <repo> <tag> <service>...
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
repo="$1"
tag="$2"
shift 2
fail=0

# Base image layers are reviewed upstream; only review layers our Dockerfile
# adds (the distroless base history has lines like "bazel build //common:passwd").
base="$(awk '/^FROM gcr.io\/distroless/ {print $2}' "$ROOT/build/Dockerfile")"
docker pull --quiet --platform linux/arm64 "$base" >/dev/null
base_history="$(docker history --no-trunc --format '{{.CreatedBy}}' "$base")"
for svc in "$@"; do
  img="$repo/$svc:$tag"
  echo "==> $img"
  if ! trivy image --quiet --scanners vuln,secret --severity HIGH,CRITICAL --ignore-unfixed \
    --exit-code 1 --format table "$img"; then
    echo "FAIL: $img has fixable HIGH/CRITICAL vulnerabilities or secrets" >&2
    fail=1
  fi
  total="$(trivy image --quiet --scanners vuln --format json "$img" | jq '[.Results[]?.Vulnerabilities[]?] | length')"
  echo "    vulnerabilities (all severities, fixed or not): $total"

  history="$(docker history --no-trunc --format '{{.CreatedBy}}' "$img" | grep -vxF -f <(printf '%s\n' "$base_history") || true)"
  if grep -Eiq 'secret|passw|token|api[_-]?key|AKIA[A-Z0-9]{8}|shortener_(dev|prod)_' <<<"$history"; then
    echo "FAIL: $img build history contains secret-looking content" >&2
    fail=1
  fi
  args="$(grep -Eo '\b[A-Z_]+=' <<<"$history" | sort -u | tr -d '=' | tr '\n' ' ' || true)"
  for a in $args; do
    case "$a" in
      SERVICE | VERSION | TARGETOS | TARGETARCH | GOTOOLCHAIN | GOFLAGS | CGO_ENABLED | GOOS | GOARCH | PATH) ;;
      *) echo "FAIL: unexpected build arg/env '$a' in $img history" >&2; fail=1 ;;
    esac
  done
  echo "    build history args: ${args:-none}"
done
exit "$fail"
