#!/usr/bin/env bash
# terraform plan -out=tfplan for one root (read-only; never applies).
# Usage: scripts/tf-plan.sh <bootstrap|dev|prod>
#
# With infra/<root>/backend.hcl present (after make bootstrap) the plan uses the
# real S3 backend. Without it ("local mode": nothing bootstrapped yet) the root
# is copied to a scratch directory with a temporary local backend override, so
# the plan shows everything that would be created. Variables come from
# terraform.tfvars if present, else example.tfvars.
# Outputs (gitignored): <root>/tfplan, tfplan.json, plan-summary.txt.
# The plan must pass scripts/check-plan-invariants.sh (log groups, alarms, topics).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH" TF_IN_AUTOMATION=1
export TF_PLUGIN_CACHE_DIR="$ROOT/.tools/tf-plugin-cache"
mkdir -p "$TF_PLUGIN_CACHE_DIR"

target="${1:?usage: tf-plan.sh <bootstrap|dev|prod>}"
case "$target" in
  bootstrap) rel="infra/bootstrap" ;;
  dev | prod) rel="infra/envs/$target" ;;
  *) echo "unknown root: $target" >&2; exit 2 ;;
esac
dir="$ROOT/$rel"
aws sts get-caller-identity >/dev/null 2>&1 || { echo "AWS credentials unavailable: plan needs read access (local mode: record as pending)" >&2; exit 1; }

varfile="terraform.tfvars"
[[ -f "$dir/$varfile" ]] || varfile="example.tfvars"

if [[ -f "$dir/backend.hcl" ]]; then
  work="$dir"
  (cd "$work" && terraform init -input=false -lockfile=readonly -backend-config=backend.hcl >/dev/null)
  mode="s3 backend"
else
  scratch="$(mktemp -d)"
  trap 'rm -rf "$scratch"' EXIT
  cp -r "$ROOT/infra" "$scratch/infra"
  find "$scratch/infra" -name .terraform -type d -prune -exec rm -rf {} +
  work="$scratch/$rel"
  printf 'terraform {\n  backend "local" {}\n}\n' >"$work/backend_override.tf"
  (cd "$work" && terraform init -input=false -lockfile=readonly >/dev/null)
  mode="local mode (no backend.hcl: empty state, temporary local backend)"
fi

(cd "$work" && terraform plan -input=false -lock=false -var-file="$varfile" -out=tfplan -no-color >"$work/plan.log" 2>&1) ||
  { tail -40 "$work/plan.log" >&2; exit 1; }
(cd "$work" && terraform show -json tfplan >tfplan.json)
if [[ "$work" != "$dir" ]]; then
  cp "$work/tfplan" "$work/tfplan.json" "$dir/"
fi

summary="$(jq -r '
  [.resource_changes[] | select(.mode == "managed") | .change.actions | join(",")] as $a |
  "create=\([$a[] | select(. == "create")] | length) update=\([$a[] | select(. == "update")] | length) " +
  "replace=\([$a[] | select(test("delete,create|create,delete"))] | length) delete=\([$a[] | select(. == "delete")] | length)"
' "$dir/tfplan.json")"
{
  echo "root:     $rel"
  echo "mode:     $mode"
  echo "vars:     $varfile"
  echo "changes:  $summary"
  echo "resource types:"
  jq -r '[.resource_changes[] | select(.mode == "managed" and .change.actions != ["no-op"]) | .type] | group_by(.) | map("  \(length)\t\(.[0])") | .[]' "$dir/tfplan.json"
} | tee "$dir/plan-summary.txt"
"$ROOT/scripts/check-plan-invariants.sh" "$dir/tfplan.json" | tee -a "$dir/plan-summary.txt"
