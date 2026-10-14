#!/usr/bin/env bash
# Pushes the arm64 images for the current commit to the environment's ECR
# repositories (immutable tags = git SHA). Requires APPROVED=yes (AGENTS.md §4).
# Usage: APPROVED=yes scripts/push-images.sh <dev|prod>
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_name="${1:?usage: push-images.sh <dev|prod>}"
project="${PROJECT:-shortener}"
region="${AWS_REGION:-us-east-1}"
services=(redirect api worker nightly)

tag="$(git -C "$ROOT" rev-parse --short=12 HEAD)"
if [[ -n "$(git -C "$ROOT" status --porcelain)" ]]; then
  echo "refusing: working tree is dirty; images must correspond to a commit" >&2
  exit 1
fi
[[ "${APPROVED:-}" == "yes" ]] || { echo "refusing: human approval required (APPROVED=yes)" >&2; exit 1; }

account="$(aws sts get-caller-identity --query Account --output text)"
registry="$account.dkr.ecr.$region.amazonaws.com"
aws ecr get-login-password --region "$region" | docker login --username AWS --password-stdin "$registry" >/dev/null

for svc in "${services[@]}"; do
  local_ref="$project/$svc:$tag"
  remote_ref="$registry/$project-$env_name-$svc:$tag"
  docker image inspect "$local_ref" >/dev/null 2>&1 || { echo "missing $local_ref (run make images)" >&2; exit 1; }
  [[ "$(docker image inspect --format '{{.Architecture}}' "$local_ref")" == "arm64" ]] || { echo "$local_ref is not arm64" >&2; exit 1; }
  docker tag "$local_ref" "$remote_ref"
  docker push --quiet "$remote_ref" >/dev/null
  digest="$(aws ecr describe-images --region "$region" --repository-name "$project-$env_name-$svc" --image-ids imageTag="$tag" --query 'imageDetails[0].imageDigest' --output text)"
  echo "pushed $remote_ref ($digest)"
done
echo "image_tag = \"$tag\""
