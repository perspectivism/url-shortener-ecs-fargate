#!/usr/bin/env bash
# Bootstrap lifecycle (SPEC §12.1, §19.1). All steps that change AWS require
# APPROVED=yes (AGENTS.md §4).
#
#   plan     read-only plan with a temporary local backend (no state bucket yet)
#   apply    apply the saved plan locally, then migrate bootstrap's own state
#            into the new bucket and write backend.hcl for bootstrap/dev/prod
#   unmigrate  move bootstrap state back to a local backend (teardown step)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH" TF_IN_AUTOMATION=1
dir="$ROOT/infra/bootstrap"
cd "$dir"
varfile="terraform.tfvars"
[[ -f "$varfile" ]] || { echo "create infra/bootstrap/terraform.tfvars from example.tfvars first" >&2; exit 1; }

local_backend() { printf 'terraform {\n  backend "local" {}\n}\n' >backend_override.tf; }
need_approval() {
  [[ "${APPROVED:-}" == "yes" ]] || { echo "refusing: human approval required (APPROVED=yes)" >&2; exit 1; }
}

write_backend_configs() {
  local bucket key region
  bucket="$(terraform output -raw state_bucket)"
  key="$(terraform output -raw state_kms_key_arn)"
  region="$(awk -F'"' '/^region/ {print $2}' "$varfile")"
  region="${region:-us-east-1}"
  for target in bootstrap envs/dev envs/prod; do
    local state_key="$target/terraform.tfstate"
    printf 'bucket       = "%s"\nkey          = "%s"\nregion       = "%s"\nencrypt      = true\nkms_key_id   = "%s"\nuse_lockfile = true\n' \
      "$bucket" "$state_key" "$region" "$key" >"$ROOT/infra/$target/backend.hcl"
    echo "wrote infra/$target/backend.hcl"
  done
}

case "${1:-}" in
  plan)
    if [[ -f backend.hcl ]]; then
      terraform init -input=false -lockfile=readonly -backend-config=backend.hcl >/dev/null
    else
      local_backend
      terraform init -input=false -lockfile=readonly >/dev/null
    fi
    terraform plan -input=false -var-file="$varfile" -out=tfplan
    ;;
  apply)
    need_approval
    [[ -f tfplan ]] || { echo "run make bootstrap (plan) first" >&2; exit 1; }
    terraform apply -input=false tfplan
    rm -f tfplan
    if [[ -f backend_override.tf ]]; then
      write_backend_configs
      rm -f backend_override.tf
      terraform init -input=false -migrate-state -force-copy -backend-config=backend.hcl
      rm -f terraform.tfstate terraform.tfstate.backup
      echo "bootstrap state migrated into s3://$(awk -F'"' '/^bucket/ {print $2}' backend.hcl)/bootstrap/terraform.tfstate"
    fi
    ;;
  unmigrate)
    need_approval
    local_backend
    terraform init -input=false -migrate-state -force-copy
    echo "bootstrap state is local again (infra/bootstrap/terraform.tfstate)"
    ;;
  *)
    echo "usage: bootstrap.sh plan|apply|unmigrate" >&2
    exit 2
    ;;
esac
