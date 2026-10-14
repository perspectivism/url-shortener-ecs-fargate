#!/usr/bin/env bash
# Secret-scanner self-test (SPEC §18 M0): plant a fake AWS access key in a
# throwaway git repo and assert that gitleaks (with this repo's config) fails.
# The key is generated at runtime so no key-shaped string is ever committed here.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$ROOT/.tools/bin:$PATH"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Read a finite chunk first: `tr </dev/urandom | head` dies of SIGPIPE under pipefail.
rand_upper="$(head -c 4096 /dev/urandom | LC_ALL=C tr -dc 'A-Z2-7')"
rand_mixed="$(head -c 4096 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9')"
key_id="AKIA${rand_upper:0:16}"
secret="${rand_mixed:0:40}"

fail=0
report="$tmp/report.json"

# expect DESCRIPTION WANT_LEAK gitleaks-args...  (asserts on the JSON report, not
# just the exit code, so a gitleaks usage/config error cannot pass as a detection)
expect() {
  local desc="$1" want="$2" rc=0 hits
  shift 2
  rm -f "$report"
  (cd "$tmp/repo" && gitleaks "$@" --no-banner --redact --config "$ROOT/.gitleaks.toml" \
    --report-format json --report-path "$report" >/dev/null 2>&1) || rc=$?
  if [[ ! -f "$report" ]]; then
    echo "FAIL: gitleaks produced no report ($desc, exit $rc)" >&2
    fail=1
    return
  fi
  hits="$(jq '[.[] | select(.RuleID == "aws-access-token")] | length' "$report")"
  if [[ "$want" == leak && "$rc" -eq 1 && "$hits" -ge 1 ]]; then
    echo "ok:   gitleaks flagged the planted key ($desc)"
  elif [[ "$want" == clean && "$rc" -eq 0 && "$hits" -eq 0 ]]; then
    echo "ok:   gitleaks reports clean before planting ($desc)"
  else
    echo "FAIL: $desc: want $want, got exit $rc with $hits aws-access-token finding(s)" >&2
    fail=1
  fi
}

mkdir -p "$tmp/repo"
git -C "$tmp/repo" init -q
git -C "$tmp/repo" config user.email "selftest@example.invalid"
git -C "$tmp/repo" config user.name "secret-scan self-test"
echo "nothing to see" >"$tmp/repo/README"
git -C "$tmp/repo" add README && git -C "$tmp/repo" commit -qm init

expect "negative control: clean history" clean git .

printf 'aws_access_key_id = %s\naws_secret_access_key = %s\n' "$key_id" "$secret" >"$tmp/repo/credentials.ini"
expect "working tree" leak dir .
git -C "$tmp/repo" add credentials.ini
expect "staged changes" leak git --staged .
git -C "$tmp/repo" commit -qm "plant fake key"
expect "git history" leak git .

exit "$fail"
