#!/usr/bin/env bash
# Writes the CI-side configuration for one root from environment variables (set
# from GitHub environment variables in the workflows; nothing secret):
#   infra/<root>/backend.hcl          TF_STATE_BUCKET, TF_STATE_KMS_KEY_ARN, AWS_REGION
#   infra/<root>/terraform.tfvars.json REPOSITORY, OWNER, [ALERT_EMAIL], [IMAGE_TAG]
# IMAGE_TAG set => enable_services = true (phase 2). Values are JSON-encoded by
# jq, so no input can inject HCL. Both files are gitignored.
# Usage: scripts/ci-config.sh <dev|prod>
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_name="${1:?usage: ci-config.sh <dev|prod>}"
[[ "$env_name" == dev || "$env_name" == prod ]] || { echo "unknown environment: $env_name" >&2; exit 2; }
dir="$ROOT/infra/envs/$env_name"

: "${TF_STATE_BUCKET:?TF_STATE_BUCKET (bootstrap output state_bucket)}"
: "${TF_STATE_KMS_KEY_ARN:?TF_STATE_KMS_KEY_ARN (bootstrap output state_kms_key_arn)}"
: "${REPOSITORY:?REPOSITORY (owner/name)}"
: "${OWNER:?OWNER (Owner tag)}"
region="${AWS_REGION:-us-east-1}"
# backend.hcl is HCL: allow only plain bucket/ARN/region characters (no "${").
[[ "$TF_STATE_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{2,62}$ ]] || { echo "invalid TF_STATE_BUCKET" >&2; exit 2; }
[[ "$TF_STATE_KMS_KEY_ARN" =~ ^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f-]+$ ]] || { echo "invalid TF_STATE_KMS_KEY_ARN" >&2; exit 2; }
[[ "$region" =~ ^[a-z]{2}(-[a-z]+)+-[0-9]$ ]] || { echo "invalid AWS_REGION" >&2; exit 2; }

jq -rn --arg b "$TF_STATE_BUCKET" --arg k "envs/$env_name/terraform.tfstate" --arg r "$region" --arg kms "$TF_STATE_KMS_KEY_ARN" \
  '"bucket       = \($b | tojson)\nkey          = \($k | tojson)\nregion       = \($r | tojson)\nencrypt      = true\nkms_key_id   = \($kms | tojson)\nuse_lockfile = true"' \
  >"$dir/backend.hcl"

jq -n --arg repo "$REPOSITORY" --arg owner "$OWNER" --arg region "$region" \
  --arg email "${ALERT_EMAIL:-}" --arg tag "${IMAGE_TAG:-}" '
  {repository: $repo, owner: $owner, region: $region}
  + (if $email != "" then {alert_email: $email} else {} end)
  + (if $tag != "" then {enable_services: true, image_tag: $tag} else {} end)' >"$dir/terraform.tfvars.json"

echo "wrote infra/envs/$env_name/backend.hcl and terraform.tfvars.json (services: $([[ -n "${IMAGE_TAG:-}" ]] && echo "on, image_tag $IMAGE_TAG" || echo off))"
