#!/usr/bin/env bash
# terraform plan -out=tfplan for one root (read-only; never applies).
# Usage: scripts/tf-plan.sh <bootstrap|dev|prod>
#
# With infra/<root>/backend.hcl present (after make bootstrap) the plan uses the
# real S3 backend. Without it ("local mode": nothing bootstrapped yet) the root
# is copied to a scratch directory with a temporary local backend override, so
# the plan shows everything that would be created. Variables (ADR-0028): the
# committed deploy.tfvars (envs) always, then the per-run overlay:
# terraform.tfvars.json in CI (scripts/ci-config.sh: alert_email, image_tag),
# or the gitignored terraform.tfvars locally (alert_email, phase-2 image_tag).
# Bootstrap uses its gitignored terraform.tfvars.
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

# Settings first: a rejected or missing deploy.tfvars stops the plan. (A plain
# assignment propagates the helper's exit status under set -e; mapfile reading
# from <(...) would silently ignore it.)
varfile_list="$("$ROOT/scripts/tf-varfiles.sh" "$target")"
mapfile -t varfiles <<<"$varfile_list"
var_args=()
for f in "${varfiles[@]}"; do var_args+=("-var-file=$f"); done
# CI must never plan against empty local state or without its per-run overlay.
if [[ "${CI:-}" == true ]]; then
  [[ -f "$dir/backend.hcl" ]] || { echo "CI: no backend.hcl (run scripts/ci-config.sh)" >&2; exit 1; }
  [[ "$target" == bootstrap || " ${varfiles[*]} " == *" terraform.tfvars.json "* ]] || { echo "CI: no terraform.tfvars.json (run scripts/ci-config.sh)" >&2; exit 1; }
fi
aws sts get-caller-identity >/dev/null 2>&1 || { echo "AWS credentials unavailable: plan needs read access (local mode: record as pending)" >&2; exit 1; }

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

(cd "$work" && terraform plan -input=false -lock=false "${var_args[@]}" -out=tfplan -no-color >"$work/plan.log" 2>&1) ||
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
  echo "vars:     ${varfiles[*]}"
  echo "changes:  $summary"
  echo "resource types:"
  jq -r '[.resource_changes[] | select(.mode == "managed" and .change.actions != ["no-op"]) | .type] | group_by(.) | map("  \(length)\t\(.[0])") | .[]' "$dir/tfplan.json"
} | tee "$dir/plan-summary.txt"
"$ROOT/scripts/check-plan-invariants.sh" "$dir/tfplan.json" | tee -a "$dir/plan-summary.txt"
