#!/usr/bin/env bash
# Self-test for the committed-settings guard (scripts/tf-varfiles.sh, ADR-0028):
# it rejects personal data and per-run values in deploy.tfvars, and tf-plan.sh
# and teardown.sh stop when it does (before touching AWS or Terraform).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/scripts" "$work/infra/envs/dev"
cp "$here/tf-varfiles.sh" "$here/tf-plan.sh" "$here/teardown.sh" "$work/scripts/"
cp "$here/../infra/envs/dev/variables.tf" "$work/infra/envs/dev/"
settings="$work/infra/envs/dev/deploy.tfvars"
fail=0
ok() { echo "ok   $1"; }
no() { echo "FAIL $1" >&2; fail=1; }
good='repository = "acme/repo"
owner      = "acme"
# alert_email = "ops@example.com"   (comments may mention anything)
monthly_budget_usd = 150'

printf '%s\n' "$good" >"$settings"
if out="$("$work/scripts/tf-varfiles.sh" dev)" && [[ "$out" == deploy.tfvars ]]; then ok "valid settings accepted"; else no "valid settings rejected: $out"; fi
echo 'alert_email = "x@y.z"' >"$work/infra/envs/dev/terraform.tfvars"
if [[ "$("$work/scripts/tf-varfiles.sh" dev | tr '\n' ' ')" == "deploy.tfvars terraform.tfvars " ]]; then ok "local overlay comes last"; else no "overlay order"; fi

reject() { # description, line appended to the valid settings, expected message part
  printf '%s\n%s\n' "$good" "$2" >"$settings"
  local msg
  if msg="$("$work/scripts/tf-varfiles.sh" dev 2>&1)"; then no "$1: accepted"; return; fi
  if [[ "$msg" == *"$3"* ]]; then ok "$1 rejected"; else no "$1: wrong message: $msg"; fi
}
reject "sensitive variable (alert_email)" 'alert_email = "ops@example.com"' "must not set alert_email"
reject "per-run image_tag" 'image_tag = "abc1234"' "must not set image_tag"
reject "per-run enable_services" 'enable_services = true' "must not set enable_services"
reject "email address in any value" 'owner_contact = "someone@example.org"' "email addresses"

# The callers stop on a rejected file (regression: process substitution hid the failure).
printf '%s\n%s\n' "$good" 'image_tag = "abc1234"' >"$settings"
if msg="$("$work/scripts/tf-plan.sh" dev 2>&1)"; then no "tf-plan.sh continued"; elif [[ "$msg" == *"must not set image_tag"* ]]; then ok "tf-plan.sh stops on rejected settings"; else no "tf-plan.sh failed for another reason: $msg"; fi
if msg="$("$work/scripts/teardown.sh" plan-destroy dev 2>&1)"; then no "teardown.sh continued"; elif [[ "$msg" == *"must not set image_tag"* ]]; then ok "teardown.sh stops on rejected settings"; else no "teardown.sh failed for another reason: $msg"; fi
rm -f "$settings"
if msg="$("$work/scripts/tf-plan.sh" dev 2>&1)"; then no "tf-plan.sh ran without deploy.tfvars"; elif [[ "$msg" == *"missing infra/envs/dev/deploy.tfvars"* ]]; then ok "tf-plan.sh stops without deploy.tfvars"; else no "missing settings: $msg"; fi
exit "$fail"
