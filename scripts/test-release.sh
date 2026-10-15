#!/usr/bin/env bash
# Self-test for scripts/release.sh --dry-run (version bumps and notes).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(mktemp -d)"
trap 'rm -rf "$repo"' EXIT
git -C "$repo" init -q
git -C "$repo" config user.email "release-test@example.invalid"
git -C "$repo" config user.name "release self-test"
commit() { echo "$1" >"$repo/f" && git -C "$repo" add f && git -C "$repo" commit -qm "$1"; }
printf 'redirect sha256:aaaa\napi sha256:bbbb\n' >"$repo/digests.txt"
fail=0
expect() { # description, expected first line, expected text in notes
  local out; out="$(cd "$repo" && DIGESTS_FILE=digests.txt "$here/release.sh" --dry-run)"
  if [[ "$(head -1 <<<"$out")" == "$2" && "$out" == *"$3"* ]]; then echo "ok   $1: $2"; else echo "FAIL $1: got $(head -1 <<<"$out")" >&2; fail=1; fi
}
commit "feat: first"
expect "first release" "release: v0.1.0 (previous: none)" "redirect sha256:aaaa"
git -C "$repo" tag v0.1.0
commit "fix: second"
expect "minor bump" "release: v0.2.0 (previous: v0.1.0)" "- fix: second"
git -C "$repo" tag -a v1.0.0 -m "major, cut manually"
expect "manual tag is kept" "release: v1.0.0 (previous: v0.1.0)" "- fix: second"
commit "feat: third"
expect "bump after a major" "release: v1.1.0 (previous: v1.0.0)" "- feat: third"
exit "$fail"
