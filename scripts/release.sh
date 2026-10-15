#!/usr/bin/env bash
# Creates the GitHub Release (and its tag) for the commit just deployed to prod
# (ADR-0026). Run by deploy.yml after the prod smoke tests pass.
#   - If HEAD already carries a release tag (e.g. a manual v1.0.0), it is
#     released under that tag; otherwise the minor version is bumped
#     (v0.1.0 -> v0.2.0). Major releases are cut by tagging manually.
#   - Notes list the commits since the previous release and the exact image
#     digests (dist/images/digests.txt) that are running in prod.
# Usage: scripts/release.sh [--dry-run]   (needs full git history and tags;
#        GH_TOKEN with contents: write unless --dry-run)
set -euo pipefail
dry_run=false
[[ "${1:-}" == --dry-run ]] && dry_run=true
digests="${DIGESTS_FILE:-dist/images/digests.txt}"
match='v[0-9]*.[0-9]*.[0-9]*'

sha="$(git rev-parse HEAD)"
if tag="$(git describe --tags --exact-match --match "$match" HEAD 2>/dev/null)"; then
  next="$tag"
  prev="$(git describe --tags --abbrev=0 --match "$match" "HEAD^" 2>/dev/null || true)"
else
  prev="$(git describe --tags --abbrev=0 --match "$match" HEAD 2>/dev/null || true)"
  IFS=. read -r major minor _ <<<"${prev:-v0.0.0}"
  next="${major}.$((minor + 1)).0"
fi
[[ "$next" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid release version: $next" >&2; exit 1; }

notes="$(mktemp)"
trap 'rm -f "$notes"' EXIT
{
  echo "Deployed to prod from commit \`${sha:0:12}\` (image tag)."
  echo
  echo "## Images (digests running in prod)"
  echo
  if [[ -f "$digests" ]]; then
    echo '```text'
    cat "$digests"
    echo '```'
  else
    echo "_digests file not found: ${digests}_"
  fi
  echo
  echo "## Changes since ${prev:-the first commit}"
  echo
  if [[ -n "$prev" ]]; then
    git log --no-merges --format='- %s (%h)' "${prev}..HEAD"
  else
    git log --no-merges --format='- %s (%h)' HEAD | head -50
  fi
} >"$notes"

if $dry_run; then
  echo "release: $next (previous: ${prev:-none})"
  cat "$notes"
  exit 0
fi
# Creates the tag at --target when it does not exist yet.
gh release create "$next" --target "$sha" --title "$next" --notes-file "$notes"
echo "released $next at ${sha:0:12}"
