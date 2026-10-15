#!/usr/bin/env bash
# Destroys environments and the bootstrap with the same plan → approve → apply
# discipline as deploys (AGENTS.md §4, SPEC §19.1). Every destroy is a saved
# plan (`terraform plan -destroy -out`); nothing is destroyed without approval.
#
#   scripts/teardown.sh plan-destroy <dev|prod>   save + summarize a destroy plan (read-only)
#   APPROVED=yes scripts/teardown.sh destroy <dev|prod>
#                                                 apply exactly that saved destroy plan
#   APPROVED=yes scripts/teardown.sh unprotect prod
#                                                 prod only, first: plan + apply force_destroy = true
#                                                 (disables deletion protection; never by editing state)
#   scripts/teardown.sh all                       interactive full teardown (prod → dev → bootstrap
#                                                 → verify); asks for a typed confirmation per step
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH" TF_IN_AUTOMATION=1

# -var-file arguments for an env root (scripts/tf-varfiles.sh, ADR-0028), in var_args.
# Fails (and stops the teardown) if the helper rejects the settings: a plain
# assignment propagates its exit status, unlike reading from <(...).
set_var_args() {
  local f list
  list="$("$ROOT/scripts/tf-varfiles.sh" "$(basename "$1")")"
  var_args=()
  while read -r f; do var_args+=("-var-file=$f"); done <<<"$list"
}

init_env() {
  local d="$1"
  [[ -f "$d/backend.hcl" ]] || { echo "no $d/backend.hcl: nothing deployed from here (remote state required)" >&2; exit 1; }
  (cd "$d" && terraform init -input=false -lockfile=readonly -backend-config=backend.hcl >/dev/null)
}

summary() {
  local d="$1" plan="$2"
  (cd "$d" && terraform show -json "$plan") | jq -r '
    [.resource_changes[] | select(.mode == "managed" and (.change.actions | index("delete")))] as $d |
    "destroys: \($d | length) resources",
    ($d | map(.type) | group_by(.) | map("  \(length)\t\(.[0])") | .[]),
    "stateful (called out, AGENTS.md §4):",
    ($d | map(select(.type | test("dynamodb_table|s3_bucket$|kms_key|elasticache_serverless_cache|secretsmanager_secret$|ecr_repository"))) | map("  ** \(.address)") | .[])'
}

need_approval() {
  [[ "${APPROVED:-}" == "yes" ]] || { echo "refusing: human approval required (re-run with APPROVED=yes after reviewing the plan)" >&2; exit 1; }
}

plan_destroy() {
  local env="$1" d="$ROOT/infra/envs/$1"
  set_var_args "$d"
  init_env "$d"
  (cd "$d" && terraform plan -destroy -input=false "${var_args[@]}" -out=tfplan-destroy -no-color >plan-destroy.log) ||
    { tail -30 "$d/plan-destroy.log" >&2; exit 1; }
  summary "$d" tfplan-destroy | tee "$d/plan-destroy-summary.txt"
  echo "saved: infra/envs/$env/tfplan-destroy (apply with: APPROVED=yes make destroy ENV=$env)"
}

destroy() {
  local env="$1" d="$ROOT/infra/envs/$1"
  [[ -f "$d/tfplan-destroy" ]] || { echo "no saved destroy plan: run make plan-destroy ENV=$env first" >&2; exit 1; }
  cat "$d/plan-destroy-summary.txt" 2>/dev/null || true
  need_approval
  if [[ "$env" == prod && "${PROD_APPLY_CONFIRMED:-}" != "yes" ]]; then
    echo "refusing to destroy prod: set PROD_APPLY_CONFIRMED=yes only on explicit owner instruction" >&2
    exit 1
  fi
  init_env "$d"
  (cd "$d" && terraform apply -input=false tfplan-destroy)
  rm -f "$d/tfplan-destroy" "$d/plan-destroy.log" "$d/plan-destroy-summary.txt"
}

# Prod keeps deletion protection (force_destroy = false). Teardown first applies
# force_destroy = true — a reviewed change like any other, never a state edit.
unprotect() {
  local env="$1" d="$ROOT/infra/envs/$1"
  [[ "$env" == prod ]] || { echo "unprotect is only needed for prod (dev already has force_destroy = true)" >&2; exit 2; }
  set_var_args "$d"
  init_env "$d"
  (cd "$d" && terraform plan -input=false "${var_args[@]}" -var force_destroy=true -out=tfplan-unprotect -no-color >plan-unprotect.log) ||
    { tail -30 "$d/plan-unprotect.log" >&2; exit 1; }
  (cd "$d" && terraform show -json tfplan-unprotect) | jq -r '
    [.resource_changes[] | select(.change.actions != ["no-op"] and .change.actions != ["read"])] |
    "changes: \(length)", (map("  \(.change.actions | join(",")) \(.address)") | .[])'
  need_approval
  [[ "${PROD_APPLY_CONFIRMED:-}" == "yes" ]] || { echo "refusing: PROD_APPLY_CONFIRMED=yes required" >&2; exit 1; }
  (cd "$d" && terraform apply -input=false tfplan-unprotect && rm -f tfplan-unprotect plan-unprotect.log)
}

confirm() {
  local phrase="$1" answer
  [[ -t 0 ]] || { echo "refusing: full teardown is interactive (no TTY)" >&2; exit 1; }
  read -r -p "Type '$phrase' to continue, anything else to stop: " answer
  [[ "$answer" == "$phrase" ]] || { echo "stopped by operator"; exit 1; }
}

all() {
  if [[ -f "$ROOT/infra/envs/prod/backend.hcl" ]] && (init_env "$ROOT/infra/envs/prod" && cd "$ROOT/infra/envs/prod" && terraform state list 2>/dev/null | grep -q .); then
    echo "== prod is deployed: step 1/2 disable deletion protection (force_destroy = true)"
    confirm "unprotect prod" && APPROVED=yes PROD_APPLY_CONFIRMED=yes unprotect prod
    echo "== prod: destroy plan"
    plan_destroy prod
    confirm "destroy prod" && APPROVED=yes PROD_APPLY_CONFIRMED=yes destroy prod
  fi
  if [[ -f "$ROOT/infra/envs/dev/backend.hcl" ]]; then
    echo "== dev: destroy plan"
    plan_destroy dev
    confirm "destroy dev" && APPROVED=yes destroy dev
  fi
  echo "== bootstrap: force_destroy on the state bucket, migrate state to local, destroy"
  echo "   (state bucket with all versions, OIDC provider if created here, CI roles, boundary, state key)"
  confirm "unprotect bootstrap" && APPROVED=yes "$ROOT/scripts/bootstrap.sh" unprotect
  APPROVED=yes "$ROOT/scripts/bootstrap.sh" unmigrate
  "$ROOT/scripts/bootstrap.sh" plan-destroy
  confirm "destroy bootstrap" && APPROVED=yes "$ROOT/scripts/bootstrap.sh" destroy
  echo "== verify"
  "$ROOT/scripts/verify-teardown.sh"
}

cmd="${1:-}"
case "$cmd" in
  plan-destroy | destroy | unprotect)
    env="${2:?usage: teardown.sh $cmd <dev|prod>}"
    [[ "$env" == dev || "$env" == prod ]] || { echo "unknown environment: $env" >&2; exit 2; }
    "${cmd//-/_}" "$env"
    ;;
  all) all ;;
  *) sed -n '2,15p' "${BASH_SOURCE[0]}" >&2; exit 2 ;;
esac
