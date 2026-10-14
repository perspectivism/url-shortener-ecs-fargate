#!/usr/bin/env bash
# Generates the terraform-docs section of every module/root README.
# Usage: scripts/tf-docs.sh          regenerate in place
#        scripts/tf-docs.sh --check  fail if any README is out of date
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
cd "$ROOT"
mapfile -t dirs < <(find infra/modules infra/bootstrap infra/envs -name main.tf -not -path '*/.terraform/*' -exec dirname {} \; | sort -u)
stale=0
for d in "${dirs[@]}"; do
  if [[ "${1:-}" == "--check" ]]; then
    tmp="$(mktemp -d)"
    cp -r "$d/." "$tmp/"
    terraform-docs -c .terraform-docs.yml "$tmp" >/dev/null
    if ! diff -q "$d/README.md" "$tmp/README.md" >/dev/null; then
      echo "stale: $d/README.md (run make tf-docs)" >&2
      stale=1
    fi
    rm -rf "$tmp"
  else
    terraform-docs -c .terraform-docs.yml "$d" >/dev/null
  fi
done
exit "$stale"
