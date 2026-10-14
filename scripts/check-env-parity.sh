#!/usr/bin/env bash
# dev and prod must deploy the same configuration; only variable defaults differ.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../infra/envs"
fail=0
for f in main.tf outputs.tf versions.tf example.tfvars; do
  if ! diff -q "dev/$f" "prod/$f" >/dev/null; then
    echo "FAIL: envs/dev/$f and envs/prod/$f differ" >&2
    fail=1
  fi
done
# variables.tf may differ only in default values.
if ! diff <(grep -v '^\s*default' dev/variables.tf) <(grep -v '^\s*default' prod/variables.tf) >/dev/null; then
  echo "FAIL: envs/*/variables.tf differ beyond default values" >&2
  fail=1
fi
[[ "$fail" -eq 0 ]] && echo "env parity: dev and prod roots match (defaults differ only)"
exit "$fail"
