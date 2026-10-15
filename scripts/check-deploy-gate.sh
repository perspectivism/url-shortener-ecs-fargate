#!/usr/bin/env bash
# Deploys never start automatically and reviewers approve the saved plan
# (ADR-0027, ADR-0028, ADR-0032). For .github/workflows/deploy.yml:
#   - the only trigger is workflow_dispatch, and every job runs only on main;
#   - the only GitHub environments are dev and prod (required reviewers); only
#     their jobs may push, apply or record approvals (APPROVED: "yes"), and
#     each depends on plan-<env> and applies its saved plan (saved-plan.sh get);
#   - any other job that assumes an AWS role (id-token: write) is a plan job:
#     it uses only the read-only deploy plan role and never pushes or applies.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
wf="${1:-.github/workflows/deploy.yml}"
fail=0
bad() { echo "FAIL: $*" >&2; fail=1; }

# Triggers: the keys under the top-level `on:` block.
triggers="$(awk '/^on:/ {on=1; next} on && /^[^ #]/ {exit} on && /^  [a-z_]+:/ {sub(/^  /, ""); sub(/:.*/, ""); print}' "$wf")"
[[ "$triggers" == "workflow_dispatch" ]] || bad "$wf triggers must be exactly workflow_dispatch, found: $(echo "$triggers" | tr '\n' ' ')"

# Jobs: name, id-token?, environment, main-gated?, needs, approves?, applies?, apply role?, saved-plan get?, plan role?
jobs="$(awk '
  function flush() { if (job) print job, oidc, env, main, (needs == "" ? "-" : needs), approved, applies, applyrole, getplan, planrole }
  /^jobs:/ {in_jobs=1; next}
  in_jobs && /^  [a-z][a-z0-9_-]*:$/ {flush(); job=$1; sub(/:$/, "", job); oidc="no"; env="-"; main="no"; needs=""; approved="no"; applies="no"; applyrole="no"; getplan="no"; planrole="no"}
  in_jobs && /id-token: write/ {oidc="yes"}
  in_jobs && /^    environment:/ {env=$2}
  in_jobs && /^    if: github.ref == .refs\/heads\/main./ {main="yes"}
  in_jobs && /^    needs:/ {n=$0; sub(/^    needs: *\[/, "", n); sub(/\].*/, "", n); gsub(/ /, "", n); needs=n; if (main != "yes") main="needs"}
  in_jobs && /APPROVED: "yes"/ {approved="yes"}
  in_jobs && /make (apply|push-images)/ {applies="yes"}
  in_jobs && /AWS_APPLY_ROLE_ARN/ {applyrole="yes"}
  in_jobs && /saved-plan\.sh get/ {getplan="yes"}
  in_jobs && /AWS_DEPLOY_PLAN_ROLE_ARN/ {planrole="yes"}
  END {flush()}' "$wf")"
while read -r job oidc env main needs approved applies applyrole getplan planrole; do
  [[ -n "$job" ]] || continue
  [[ "$main" != no ]] || bad "job '$job' neither checks github.ref == refs/heads/main nor depends on a job that does"
  case "$env" in
    dev | prod)
      [[ ",$needs," == *",plan-$env,"* ]] || bad "job '$job' ($env) must depend on plan-$env (approve the saved plan, not a future one)"
      if [[ "$applies" == yes && "$getplan" == no ]]; then bad "job '$job' ($env) applies without fetching the approved saved plan (scripts/saved-plan.sh get)"; fi
      [[ "$planrole" == no ]] || bad "job '$job' ($env) must use its apply role, not the deploy plan role"
      ;;
    -)
      [[ "$approved" == no && "$applies" == no && "$applyrole" == no ]] || bad "job '$job' pushes, applies or uses the apply role outside an approval-gated environment (dev/prod)"
      if [[ "$oidc" == yes && "$planrole" == no ]]; then bad "job '$job' assumes an AWS role without an environment that is not the read-only deploy plan role"; fi
      ;;
    *) bad "job '$job' uses environment '$env': only dev and prod exist (ADR-0032)" ;;
  esac
done <<<"$jobs"

[[ $fail -eq 0 ]] || exit 1
echo "deploy gate OK: manual trigger only; $(grep -c . <<<"$jobs") jobs; plans use the read-only role, applies run only in dev/prod with the approved saved plan"
