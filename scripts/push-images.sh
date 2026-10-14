#!/usr/bin/env bash
# Pushes the OCI layouts from scripts/build-oci.sh (dist/images) to the
# environment's ECR repositories, tag = git SHA, and verifies that ECR reports
# exactly the built manifest digest — the same bytes in every environment.
# Idempotent: an existing tag with the same digest is accepted; a different
# digest fails (tags are immutable). Requires APPROVED=yes (AGENTS.md §4; in
# CI the GitHub environment is the approval).
# Usage: APPROVED=yes scripts/push-images.sh <dev|prod> [registry]
#   registry defaults to <account>.dkr.ecr.<region>.amazonaws.com (ECR login
#   via the AWS CLI); pass one to test against another registry.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_name="${1:?usage: push-images.sh <dev|prod> [registry]}"
registry="${2:-}"
project="${PROJECT:-shortener}"
region="${AWS_REGION:-us-east-1}"
dir="$ROOT/dist/images"

[[ "${APPROVED:-}" == "yes" ]] || { echo "refusing: human approval required (APPROVED=yes)" >&2; exit 1; }
[[ -f "$dir/digests.txt" && -f "$dir/tag" ]] || { echo "no $dir: run make images-oci first" >&2; exit 1; }
tag="$(cat "$dir/tag")"
if [[ ! "$tag" =~ ^[0-9a-f]{12}$ && -z "$registry" ]]; then
  echo "refusing: tag $tag is not a clean commit SHA (dirty build); only test registries accept it" >&2
  exit 1
fi

if [[ -z "$registry" ]]; then
  account="$(aws sts get-caller-identity --query Account --output text)"
  registry="$account.dkr.ecr.$region.amazonaws.com"
  aws ecr get-login-password --region "$region" | crane auth login "$registry" --username AWS --password-stdin >/dev/null
fi

while read -r svc digest; do
  ref="$registry/$project-$env_name-$svc:$tag"
  if existing="$(crane digest "$ref" 2>/dev/null)"; then
    [[ "$existing" == "$digest" ]] || { echo "FAIL: $ref exists with $existing, built $digest" >&2; exit 1; }
    echo "exists  $ref@$digest"
    continue
  fi
  crane push "$dir/$svc" "$ref" >/dev/null
  got="$(crane digest "$ref")"
  [[ "$got" == "$digest" ]] || { echo "FAIL: $ref has digest $got, built $digest" >&2; exit 1; }
  echo "pushed  $ref@$digest"
done <"$dir/digests.txt"
echo "image_tag = \"$tag\""
