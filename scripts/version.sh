#!/usr/bin/env bash
# Prints the build version stamped into every binary (internal/version):
#   on a release tag        v0.2.0+3b2d556f3a67
#   after a release tag     v0.2.0-4-g3b2d556f3a67   (4 commits after v0.2.0)
#   before any release      v0.0.0-g3b2d556f3a67
#   uncommitted changes     ...-dirty
# Release tags are annotated vMAJOR.MINOR.PATCH tags (ADR-0026). The version
# is fixed at build time: images are promoted unchanged, so a commit released
# later still reports its describe form; `git describe <sha>` maps it to the
# release afterwards. The image tag stays the bare 12-character SHA.
set -euo pipefail
cd "${1:-.}"
sha="$(git rev-parse --short=12 HEAD)"
dirty=""
[[ -z "$(git status --porcelain --untracked-files=no)" ]] || dirty="-dirty"
if tag="$(git describe --tags --exact-match --match 'v[0-9]*.[0-9]*.[0-9]*' HEAD 2>/dev/null)"; then
  echo "${tag}+${sha}${dirty}"
elif desc="$(git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --abbrev=12 HEAD 2>/dev/null)"; then
  echo "${desc}${dirty}"
else
  echo "v0.0.0-g${sha}${dirty}"
fi
