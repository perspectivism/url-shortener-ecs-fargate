#!/usr/bin/env bash
# Self-test for scripts/version.sh against a throwaway repository.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ver="$here/version.sh"
repo="$(mktemp -d)"
trap 'rm -rf "$repo"' EXIT
git -C "$repo" init -q
git -C "$repo" config user.email "version-test@example.invalid"
git -C "$repo" config user.name "version self-test"
commit() { echo "$1" >"$repo/f" && git -C "$repo" add f && git -C "$repo" commit -qm "$1"; }
fail=0
expect() { # description, regex
  local got; got="$("$ver" "$repo")"
  if [[ "$got" =~ $2 ]]; then echo "ok   $1: $got"; else echo "FAIL $1: $got !~ $2" >&2; fail=1; fi
}
commit one
expect "no release yet" '^v0\.0\.0-g[0-9a-f]{12}$'
git -C "$repo" tag -a v0.1.0 -m "v0.1.0"
expect "on a release tag" '^v0\.1\.0\+[0-9a-f]{12}$'
git -C "$repo" tag gate-m1 # non-release tags are ignored
commit two; commit three
expect "two commits after" '^v0\.1\.0-2-g[0-9a-f]{12}$'
echo change >"$repo/f"
expect "dirty tree" '^v0\.1\.0-2-g[0-9a-f]{12}-dirty$'
git -C "$repo" checkout -q f
git -C "$repo" tag -a v0.2.0 -m "v0.2.0"
expect "newest release wins" '^v0\.2\.0\+[0-9a-f]{12}$'
exit "$fail"
