#!/usr/bin/env bash
# Smoke tests against a deployed environment, with URLs from its Terraform
# outputs (read-only): base_url through CloudFront, and the internal ALB DNS
# name, which must NOT answer (TestALBNotPublic). SMOKE_API_KEY (and optionally
# SMOKE_API_KEY_2) come from the caller's environment — in CI from GitHub
# environment secrets — never from files or arguments.
# Usage: scripts/smoke-env.sh <dev|prod>
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH" TF_IN_AUTOMATION=1
env_name="${1:?usage: smoke-env.sh <dev|prod>}"
dir="$ROOT/infra/envs/$env_name"

: "${SMOKE_API_KEY:?export SMOKE_API_KEY (links:read, links:write) for $env_name}"
[[ -f "$dir/backend.hcl" ]] || { echo "no backend.hcl for $env_name" >&2; exit 1; }
(cd "$dir" && terraform init -input=false -lockfile=readonly -backend-config=backend.hcl >/dev/null)
outputs="$(cd "$dir" && terraform output -json)"
base_url="$(jq -r '.base_url.value // empty' <<<"$outputs")"
alb_dns="$(jq -r '.alb_dns_name.value // empty' <<<"$outputs")"
scanning="$(jq -r '.url_scanning_enabled.value // false' <<<"$outputs")"
[[ -n "$base_url" ]] || { echo "$env_name has no base_url (services not enabled?)" >&2; exit 1; }

echo "smoke: $env_name at $base_url (scanning: $scanning)"
cd "$ROOT"
SMOKE_BASE_URL="$base_url" SMOKE_ALB_DNS="$alb_dns" SMOKE_EXPECT_SCANNING="$scanning" \
  go test -count=1 -tags=smoke -timeout=10m -v ./tests/smoke/...
