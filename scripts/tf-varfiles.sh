#!/usr/bin/env bash
# Prints the -var-file list (one per line, relative to the root) for a root,
# in precedence order, and enforces what may live where (ADR-0028):
#   envs: deploy.tfvars (committed, required) + terraform.tfvars.json (CI
#         overlay) or terraform.tfvars (local overlay, gitignored)
#   bootstrap: terraform.tfvars (gitignored)
# deploy.tfvars must not set alert_email (personal data stays out of git) or
# image_tag (chosen per run).
# Usage: scripts/tf-varfiles.sh <bootstrap|dev|prod>
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${1:?usage: tf-varfiles.sh <bootstrap|dev|prod>}"
case "$target" in
  bootstrap)
    [[ -f "$ROOT/infra/bootstrap/terraform.tfvars" ]] || { echo "create infra/bootstrap/terraform.tfvars from example.tfvars first" >&2; exit 1; }
    echo terraform.tfvars
    exit 0
    ;;
  dev | prod) dir="$ROOT/infra/envs/$target" ;;
  *) echo "unknown root: $target" >&2; exit 2 ;;
esac
[[ -f "$dir/deploy.tfvars" ]] || { echo "missing infra/envs/$target/deploy.tfvars (committed settings)" >&2; exit 1; }
if grep -Eq '^[[:space:]]*(alert_email|image_tag)[[:space:]]*=' "$dir/deploy.tfvars"; then
  echo "infra/envs/$target/deploy.tfvars must not set alert_email or image_tag (overlay files only)" >&2
  exit 1
fi
echo deploy.tfvars
if [[ -f "$dir/terraform.tfvars.json" ]]; then
  echo terraform.tfvars.json
elif [[ -f "$dir/terraform.tfvars" ]]; then
  echo terraform.tfvars
fi
