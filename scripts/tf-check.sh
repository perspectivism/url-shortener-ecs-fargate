#!/usr/bin/env bash
# Static checks for all Terraform (SPEC §18 M4): fmt, validate, tflint,
# checkov, Trivy config, terraform test, plan/metric/IAM self-checks, dev/prod
# parity and README freshness.
# Never touches AWS.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
export TFLINT_PLUGIN_DIR="$ROOT/.tools/tflint-plugins"
export TF_IN_AUTOMATION=1
export TF_PLUGIN_CACHE_DIR="$ROOT/.tools/tf-plugin-cache"
mkdir -p "$TF_PLUGIN_CACHE_DIR"
cd "$ROOT"

step() { printf '\n==> %s\n' "$*"; }

roots=(infra/bootstrap infra/envs/dev infra/envs/prod)
mapfile -t modules < <(find infra/modules -mindepth 1 -maxdepth 1 -type d | sort)

step "terraform fmt -check -recursive"
terraform fmt -check -recursive infra

step "terraform validate (${#roots[@]} roots, ${#modules[@]} modules)"
# Roots: the committed lock file must already pin every provider (readonly).
for d in "${roots[@]}"; do
  (cd "$d" && terraform init -backend=false -input=false -lockfile=readonly >/dev/null && terraform validate -no-color >/dev/null) ||
    { echo "validate failed: $d" >&2; exit 1; }
  echo "ok   $d"
done
# Child modules carry no lock file (they inherit the root's).
for d in "${modules[@]}"; do
  (cd "$d" && terraform init -backend=false -input=false >/dev/null && terraform validate -no-color >/dev/null) ||
    { echo "validate failed: $d" >&2; exit 1; }
  echo "ok   $d"
done

step "tflint (AWS ruleset + all core rules)"
tflint --init --config "$ROOT/.tflint.hcl" >/dev/null
tflint --recursive --config "$ROOT/.tflint.hcl" --format compact
echo "tflint: no issues"

step "checkov"
checkov -d infra --framework terraform --quiet --compact --skip-download

step "trivy config (MEDIUM, HIGH, CRITICAL)"
# Saved plans (gitignored) embed stale source snapshots; scan the source tree only.
trivy config --quiet --severity MEDIUM,HIGH,CRITICAL --exit-code 1 --skip-files "**/tfplan" infra

step "terraform test"
for d in "${modules[@]}" infra/bootstrap infra/envs/dev infra/envs/prod; do
  if ! compgen -G "$d/tests/*.tftest.hcl" >/dev/null; then
    echo "FAIL: $d has no tests" >&2
    exit 1
  fi
  out="$(cd "$d" && terraform test -no-color 2>&1)" || { echo "$out" >&2; echo "terraform test failed: $d" >&2; exit 1; }
  echo "ok   $d: $(grep -E '^(Success|Failure)!' <<<"$out")"
done

step "plan invariants (self-test) and EMF metrics"
scripts/check-plan-invariants.sh scripts/testdata/plan-invariants-good.json >/dev/null
if bad="$(scripts/check-plan-invariants.sh scripts/testdata/plan-invariants-bad.json 2>&1)"; then
  echo "FAIL: check-plan-invariants.sh accepted a bad plan" >&2
  exit 1
fi
[[ "$(grep -c ': ' <<<"$bad")" -ge 4 ]] || { echo "$bad" >&2; echo "FAIL: not every violation was reported" >&2; exit 1; }
echo "check-plan-invariants.sh rejects all 4 violations in the bad fixture"
scripts/check-metrics.sh

step "security review (offline): suppressions, IAM wildcards, validate-iam self-test"
python3 -I scripts/check_suppressions.py
python3 -I scripts/check_iam_wildcards.py infra
if python3 -I scripts/check_iam_wildcards.py scripts/testdata/iam-wildcards-bad.tf.txt 2>/dev/null; then
  echo "FAIL: check_iam_wildcards.py accepted the bad fixture" >&2
  exit 1
fi
viam="$(scripts/validate-iam.sh scripts/testdata/validate-iam-plan.json --dry-run --allow-unknown | tail -1)"
[[ "$viam" == "policies: 3, validated: 0, failed: 0, unknown until apply: 1 (dry run)" ]] ||
  { echo "FAIL: validate-iam.sh extraction self-test: $viam" >&2; exit 1; }
echo "validate-iam.sh extracts the fixture's 3 policies (1 unknown); Access Analyzer itself runs via make validate-iam"

step "dev/prod parity"
scripts/check-env-parity.sh

step "module README freshness"
scripts/tf-docs.sh --check
echo "READMEs up to date"
