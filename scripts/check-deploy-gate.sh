#!/usr/bin/env bash
# Deploys never start automatically and reviewers approve the saved plan
# (ADR-0027, ADR-0028). For .github/workflows/deploy.yml:
#   - the only trigger is workflow_dispatch, and every job runs only on main;
#   - a job that assumes an AWS role (id-token: write) runs in a GitHub
#     environment: <env>-plan (read-only deploy plan role) or <env> (apply role,
#     required reviewers);
#   - only <env> jobs record approvals (APPROVED: "yes") and they depend on
#     plan-<env>, whose saved plan they apply (scripts/saved-plan.sh get);
#   - <env>-plan jobs never push, apply or assume the apply role.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
wf="${1:-.github/workflows/deploy.yml}"
fail=0
bad() { echo "FAIL: $*" >&2; fail=1; }

# Triggers: the keys under the top-level `on:` block.
triggers="$(awk '/^on:/ {on=1; next} on && /^[^ #]/ {exit} on && /^  [a-z_]+:/ {sub(/^  /, ""); sub(/:.*/, ""); print}' "$wf")"
[[ "$triggers" == "workflow_dispatch" ]] || bad "$wf triggers must be exactly workflow_dispatch, found: $(echo "$triggers" | tr '\n' ' ')"

# Jobs: name, id-token?, environment, main-gated?, needs, approves?, applies?, apply role?, saved-plan get?
jobs="$(awk '
  function flush() { if (job) print job, oidc, env, main, (needs == "" ? "-" : needs), approved, applies, applyrole, getplan }
  /^jobs:/ {in_jobs=1; next}
  in_jobs && /^  [a-z][a-z0-9_-]*:$/ {flush(); job=$1; sub(/:$/, "", job); oidc="no"; env="-"; main="no"; needs=""; approved="no"; applies="no"; applyrole="no"; getplan="no"}
  in_jobs && /id-token: write/ {oidc="yes"}
  in_jobs && /^    environment:/ {env=$2}
  in_jobs && /^    if: github.ref == .refs\/heads\/main./ {main="yes"}
  in_jobs && /^    needs:/ {n=$0; sub(/^    needs: *\[/, "", n); sub(/\].*/, "", n); gsub(/ /, "", n); needs=n; if (main != "yes") main="needs"}
  in_jobs && /APPROVED: "yes"/ {approved="yes"}
  in_jobs && /make (apply|push-images)/ {applies="yes"}
  in_jobs && /AWS_APPLY_ROLE_ARN/ {applyrole="yes"}
  in_jobs && /saved-plan\.sh get/ {getplan="yes"}
  END {flush()}' "$wf")"
while read -r job oidc env main needs approved applies applyrole getplan; do
  [[ -n "$job" ]] || continue
  [[ "$main" != no ]] || bad "job '$job' neither checks github.ref == refs/heads/main nor depends on a job that does"
  case "$env" in
    dev-plan | prod-plan)
      [[ "$approved" == no && "$applies" == no && "$applyrole" == no ]] || bad "plan job '$job' ($env) must not push, apply or assume the apply role"
      ;;
    dev | prod)
      [[ ",$needs," == *",plan-$env,"* ]] || bad "job '$job' ($env) must depend on plan-$env (approve the saved plan, not a future one)"
      if [[ "$applies" == yes && "$getplan" == no ]]; then bad "job '$job' ($env) applies without fetching the approved saved plan (scripts/saved-plan.sh get)"; fi
      ;;
    *)
      [[ "$oidc" == no ]] || bad "job '$job' assumes an AWS role outside the dev/prod(-plan) environments"
      [[ "$approved" == no && "$applies" == no ]] || bad "job '$job' pushes or applies outside an approval-gated environment"
      ;;
  esac
done <<<"$jobs"

[[ $fail -eq 0 ]] || exit 1
echo "deploy gate OK: manual trigger only; $(grep -c . <<<"$jobs") jobs; AWS roles only in <env>-plan / <env> environments; every apply uses the approved saved plan"
