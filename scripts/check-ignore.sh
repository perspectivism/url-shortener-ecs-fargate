#!/usr/bin/env bash
# Asserts that secret-bearing paths are gitignored and committed examples are not
# (SPEC §11.2, §18 M0).
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

must_ignore=(
  .env
  .env.local
  .env.dev
  infra/envs/dev/terraform.tfvars
  infra/envs/prod/terraform.tfvars
  infra/envs/dev/backend.hcl
  backend.hcl
  x.tfstate
  infra/bootstrap/terraform.tfstate.backup
  infra/envs/dev/.terraform/providers/x
  infra/envs/dev/tfplan
  infra/envs/dev/tfplan.json
  infra/envs/dev/plan.log
  infra/envs/dev/plan-summary.txt
  infra/envs/dev/tfplan-destroy
  infra/envs/dev/plan-destroy.log
  infra/envs/dev/plan-destroy-summary.txt
  infra/envs/prod/tfplan-unprotect
  infra/envs/prod/plan-unprotect.log
  infra/bootstrap/tfplan-destroy
  infra/bootstrap/tfplan-unprotect
  infra/bootstrap/backend_override.tf
  infra/envs/dev/terraform.tfvars.json
  infra/bootstrap/terraform.tfvars
  dist/images/redirect/index.json
  coverage/unit.out
  tests/load/results/summary.json
  .docker/config.json
  server.pem
  private.key
)
must_track=(
  .env.example
  example.tfvars
  infra/envs/dev/deploy.tfvars
  infra/envs/prod/deploy.tfvars
  infra/envs/dev/backend.hcl.example
)

fail=0
for p in "${must_ignore[@]}"; do
  if git check-ignore --no-index -q "$p"; then echo "ok:   ignored     $p"; else echo "FAIL: not ignored $p" >&2; fail=1; fi
done
for p in "${must_track[@]}"; do
  if git check-ignore --no-index -q "$p"; then echo "FAIL: ignored     $p (must be committable)" >&2; fail=1; else echo "ok:   trackable   $p"; fi
done
exit "$fail"
