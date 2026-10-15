#!/usr/bin/env bash
# Deploys never start automatically (ADR-0027): .github/workflows/deploy.yml
# may only be triggered by workflow_dispatch, its jobs run only on main, and
# every job that assumes an AWS role (id-token: write) runs in a GitHub
# environment (dev/prod), whose required reviewers approve it.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
wf=.github/workflows/deploy.yml
fail=0
bad() { echo "FAIL: $*" >&2; fail=1; }

# Triggers: the keys under the top-level `on:` block.
triggers="$(awk '/^on:/ {on=1; next} on && /^[^ #]/ {exit} on && /^  [a-z_]+:/ {sub(/^  /, ""); sub(/:.*/, ""); print}' "$wf")"
[[ "$triggers" == "workflow_dispatch" ]] || bad "$wf triggers must be exactly workflow_dispatch, found: $(echo "$triggers" | tr '\n' ' ')"

# Jobs: name, whether it requests id-token: write, its environment, its if:.
jobs="$(awk '
  /^jobs:/ {in_jobs=1; next}
  in_jobs && /^  [a-z][a-z0-9_-]*:$/ {if (job) print job, oidc, env, main; job=$1; sub(/:$/, "", job); oidc="no"; env="-"; main="no"}
  in_jobs && /id-token: write/ {oidc="yes"}
  in_jobs && /^    environment:/ {env=$2}
  in_jobs && /^    if: github.ref == .refs\/heads\/main./ {main="yes"}
  in_jobs && /^    needs:/ {main=(main=="yes" ? "yes" : "needs")}
  END {if (job) print job, oidc, env, main}' "$wf")"
while read -r job oidc env main; do
  [[ -n "$job" ]] || continue
  if [[ "$oidc" == yes && "$env" != dev && "$env" != prod ]]; then
    bad "job '$job' assumes an AWS role without an approval-gated environment (dev/prod)"
  fi
  [[ "$main" != no ]] || bad "job '$job' neither checks github.ref == refs/heads/main nor depends on a job that does"
done <<<"$jobs"

[[ $fail -eq 0 ]] || exit 1
echo "deploy gate OK: manual trigger only; $(grep -c . <<<"$jobs") jobs, every AWS role behind an approval-gated environment"
