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
  server.pem
  private.key
)
must_track=(
  .env.example
  example.tfvars
  infra/envs/dev/example.tfvars
  infra/envs/prod/example.tfvars
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
