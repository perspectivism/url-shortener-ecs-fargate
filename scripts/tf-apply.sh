#!/usr/bin/env bash
# Applies the SAVED plan (infra/<root>/tfplan) so what was approved is exactly
# what runs (AGENTS.md §4). Refuses without APPROVED=yes and a real backend.
# Usage: APPROVED=yes scripts/tf-apply.sh <dev|prod>
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH" TF_IN_AUTOMATION=1
env_name="${1:?usage: tf-apply.sh <dev|prod>}"
dir="$ROOT/infra/envs/$env_name"

[[ -f "$dir/backend.hcl" ]] || { echo "no backend.hcl: run make bootstrap first (remote state required for apply)" >&2; exit 1; }
[[ -f "$dir/tfplan" ]] || { echo "no saved plan: run make plan ENV=$env_name first" >&2; exit 1; }
cat "$dir/plan-summary.txt" 2>/dev/null || true
if [[ "${APPROVED:-}" != "yes" ]]; then
  echo "refusing to apply: human approval required (re-run with APPROVED=yes after approving this exact plan)" >&2
  exit 1
fi
if [[ "$env_name" == "prod" && "${PROD_APPLY_CONFIRMED:-}" != "yes" ]]; then
  echo "refusing to apply prod: set PROD_APPLY_CONFIRMED=yes only on explicit owner instruction" >&2
  exit 1
fi
cd "$dir"
terraform init -input=false -lockfile=readonly -backend-config=backend.hcl >/dev/null
terraform apply -input=false tfplan
rm -f tfplan tfplan.json plan-summary.txt
