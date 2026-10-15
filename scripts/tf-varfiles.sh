#!/usr/bin/env bash
# Prints the -var-file list (one per line, relative to the root) for a root,
# in precedence order, and enforces what may live where (ADR-0028):
#   envs: deploy.tfvars (committed, required) + terraform.tfvars.json (CI
#         overlay) or terraform.tfvars (local overlay, gitignored)
#   bootstrap: terraform.tfvars (gitignored)
# deploy.tfvars is committed, so it must hold no personal data or per-run
# values: it may not set a variable marked `sensitive` in variables.tf (e.g.
# alert_email), contain anything that looks like an email address, or set
# image_tag / enable_services (chosen per run).
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
settings="$dir/deploy.tfvars"
# Variables declared sensitive in variables.tf.
mapfile -t sensitive < <(awk '/^variable "/ {name=$2; gsub(/"/, "", name)} /^[[:space:]]*sensitive[[:space:]]*=[[:space:]]*true/ {print name}' "$dir/variables.tf")
for v in "${sensitive[@]}" image_tag enable_services; do
  if grep -Eq "^[[:space:]]*${v}[[:space:]]*=" "$settings"; then
    echo "infra/envs/$target/deploy.tfvars must not set $v (sensitive or per-run: use the gitignored overlay)" >&2
    exit 1
  fi
done
if grep -v '^[[:space:]]*#' "$settings" | grep -Eq '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'; then
  echo "infra/envs/$target/deploy.tfvars must not contain email addresses (committed file)" >&2
  exit 1
fi
echo deploy.tfvars
if [[ -f "$dir/terraform.tfvars.json" ]]; then
  echo terraform.tfvars.json
elif [[ -f "$dir/terraform.tfvars" ]]; then
  echo terraform.tfvars
fi
