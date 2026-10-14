#!/usr/bin/env bash
# dev and prod must deploy the same configuration; only variable defaults differ.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../infra/envs"
fail=0
mapfile -t files < <(cd dev && find . -maxdepth 1 \( -name '*.tf' -o -name example.tfvars \) ! -name variables.tf -printf '%f\n' | sort)
mapfile -t prod_files < <(cd prod && find . -maxdepth 1 \( -name '*.tf' -o -name example.tfvars \) ! -name variables.tf -printf '%f\n' | sort)
if [[ "${files[*]}" != "${prod_files[*]}" ]]; then
  echo "FAIL: envs/dev and envs/prod contain different files" >&2
  fail=1
fi
for f in "${files[@]}"; do
  if ! diff -q "dev/$f" "prod/$f" >/dev/null 2>&1; then
    echo "FAIL: envs/dev/$f and envs/prod/$f differ" >&2
    fail=1
  fi
done
# variables.tf may differ only in default values.
if ! diff <(grep -v '^\s*default' dev/variables.tf) <(grep -v '^\s*default' prod/variables.tf) >/dev/null; then
  echo "FAIL: envs/*/variables.tf differ beyond default values" >&2
  fail=1
fi
[[ "$fail" -eq 0 ]] && echo "env parity: ${#files[@]} files identical; variables.tf differs only in defaults"
exit "$fail"
