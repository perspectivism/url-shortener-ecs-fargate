#!/usr/bin/env bash
# HUMAN-RUN (make seed-secrets ENV=dev): writes secret VALUES into the secret
# containers Terraform created (ADR-0014). Values flow human -> Secrets
# Manager -> ECS only: they are never echoed, logged, written to disk, passed
# on a command line (visible in ps) or stored in Terraform state.
#
#   visitor-hash-salt      generated with secretsmanager get-random-password
#   safe-browsing-api-key  prompted (hidden input); may be skipped when URL
#                          scanning is disabled (dev default)
#
# Idempotent: secrets that already have a current version are skipped unless
# --rotate is given. Existence checks read metadata only (never the value).
set -euo pipefail

env_name="${1:?usage: seed-secrets.sh <dev|prod> [--rotate]}"
rotate=false
[[ "${2:-}" == "--rotate" ]] && rotate=true
project="${PROJECT:-shortener}"
region="${AWS_REGION:-us-east-1}"
prefix="$project/$env_name"

aws sts get-caller-identity --region "$region" >/dev/null

has_value() {
  local n
  n="$(aws secretsmanager list-secret-version-ids --region "$region" --secret-id "$1" \
    --query "length(Versions[?contains(VersionStages, \`AWSCURRENT\`)])" --output text)"
  [[ "$n" -gt 0 ]]
}

# put SECRET_ID — reads the value from stdin; passes it via a /dev/fd path.
put() {
  local value
  IFS= read -r value
  # printf is a shell builtin, so the value never appears in a process argv.
  aws secretsmanager put-secret-value --region "$region" --secret-id "$1" \
    --secret-string "file://"<(printf '%s' "$value") >/dev/null
  value=""
}

salt_id="$prefix/visitor-hash-salt"
if has_value "$salt_id" && ! $rotate; then
  echo "skip  $salt_id (already set; use --rotate to replace — this resets unique-visitor continuity)"
else
  aws secretsmanager get-random-password --region "$region" --password-length 64 \
    --exclude-punctuation --require-each-included-type --query RandomPassword --output text | put "$salt_id"
  echo "set   $salt_id (generated, 64 chars)"
fi

key_id="$prefix/safe-browsing-api-key"
if has_value "$key_id" && ! $rotate; then
  echo "skip  $key_id (already set; use --rotate to replace)"
else
  printf 'Google Safe Browsing API key for %s (input hidden; empty to skip): ' "$env_name" >&2
  IFS= read -rs key
  echo >&2
  if [[ -z "$key" ]]; then
    echo "skip  $key_id (no value entered; URL scanning stays disabled until set)"
  else
    printf '%s\n' "$key" | put "$key_id"
    key=""
    echo "set   $key_id"
  fi
fi

echo "done. Verify (metadata only): aws secretsmanager list-secret-version-ids --secret-id <name>"
