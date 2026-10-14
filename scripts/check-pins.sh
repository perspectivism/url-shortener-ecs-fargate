#!/usr/bin/env bash
# Supply-chain pins (AGENTS.md §3, M8 gate):
#   - every `uses:` in .github/workflows is owner/repo[/path]@<40-hex SHA> with a
#     "# vX.Y.Z" comment (local ./ actions excepted; docker:// needs @sha256)
#   - every FROM in build/Dockerfile and every compose image is pinned by digest
#   - .tool-versions entries are exact versions; nothing anywhere says "latest"
#     (except the Terraform validations that reject ":latest" image tags)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
fail=0 n_uses=0 n_images=0
bad() { echo "FAIL: $*" >&2; fail=1; }

shopt -s nullglob
for f in .github/workflows/*.yml .github/workflows/*.yaml .github/actions/*/action.yml; do
  while IFS= read -r line; do
    n="${line%%:*}" ref="$(sed -E 's/^[0-9]+:\s*-?\s*uses:\s*//; s/\s+#.*$//; s/["'\'']//g' <<<"$line")"
    n_uses=$((n_uses + 1))
    case "$ref" in
      ./*) ;;
      docker://*) [[ "$ref" =~ @sha256:[0-9a-f]{64}$ ]] || bad "$f:$n: $ref is not pinned by digest" ;;
      *) [[ "$ref" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$ ]] || bad "$f:$n: $ref is not pinned to a full commit SHA"
         [[ "$line" =~ \#\ *v[0-9]+(\.[0-9]+)*$ ]] || bad "$f:$n: pinned SHA needs a '# vX.Y.Z' version comment" ;;
    esac
  done < <(grep -nE '^\s*-?\s*uses:' "$f" || true)
done

while IFS= read -r line; do
  n_images=$((n_images + 1))
  [[ "$line" =~ @sha256:[0-9a-f]{64} ]] || bad "build/Dockerfile: ${line#*:} is not pinned by digest"
done < <(grep -nE '^FROM ' build/Dockerfile)

while IFS= read -r line; do
  n_images=$((n_images + 1))
  [[ "$line" =~ @sha256:[0-9a-f]{64} ]] || bad "docker-compose.yml:${line%%:*}: image not pinned by digest"
done < <(grep -nE '^\s*image:' docker-compose.yml | grep -vE 'image:\s*\$\{|image:\s*shortener/')

while read -r tool version; do
  [[ -z "$tool" || "$tool" == \#* ]] && continue
  [[ "$version" =~ ^[0-9]+(\.[0-9]+)+$ ]] || bad ".tool-versions: $tool $version is not an exact version"
done <.tool-versions

if grep -nE '(:latest\b|@latest\b|version:\s*latest)' -r .github build docker-compose.yml Makefile scripts .tool-versions infra \
  --include='*.yml' --include='*.yaml' --include='Dockerfile' --include='Makefile' --include='*.sh' --include='*.tf' --include='.tool-versions' 2>/dev/null |
  grep -v 'scripts/check-pins.sh' | grep -vF '!endswith(var.image, ":latest")'; then
  bad "'latest' reference found (above)"
fi

[[ $fail -eq 0 ]] || exit 1
echo "pins OK: $n_uses action references (full SHAs), $n_images container images (digests), exact tool versions, no 'latest'"
