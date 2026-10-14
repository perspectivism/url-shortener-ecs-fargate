#!/usr/bin/env bash
# Trivy secret-scanner self-test: build a throwaway image containing a
# runtime-generated fake AWS key and assert Trivy reports it. Proves the image
# secret scan in scan-images.sh is not vacuous.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
tmp="$(mktemp -d)"
img="shortener/secret-scan-selftest:$$"
trap 'rm -rf "$tmp"; docker rmi -f "$img" >/dev/null 2>&1 || true' EXIT

rand="$(head -c 4096 /dev/urandom | LC_ALL=C tr -dc 'A-Z2-7')"
mixed="$(head -c 4096 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')"
printf '[default]\naws_access_key_id = AKIA%s\naws_secret_access_key = %s\n' "${rand:0:16}" "${mixed:0:40}" >"$tmp/credentials"
base="$(awk '/^FROM gcr.io\/distroless/ {print $2}' "$ROOT/build/Dockerfile")"
printf 'FROM %s\nCOPY credentials /home/nonroot/.aws/credentials\n' "$base" >"$tmp/Dockerfile"
docker buildx build --quiet --load -t "$img" "$tmp" >/dev/null

found="$(trivy image --quiet --scanners secret --format json "$img" | jq '[.Results[]?.Secrets[]?] | length')"
if [[ "$found" -ge 1 ]]; then
  echo "ok:   trivy flagged the planted key ($found finding(s))"
else
  echo "FAIL: trivy did not detect the planted key" >&2
  exit 1
fi
