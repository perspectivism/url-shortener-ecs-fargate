#!/usr/bin/env bash
# Renders every ```mermaid block in every Markdown file with @mermaid-js/mermaid-cli.
# Fails (and names the file and starting line) if any diagram does not render.
#
# Chrome runs with --no-sandbox: the input is this repository's own Markdown and
# Ubuntu 24.04 (incl. GitHub runners) blocks the unprivileged user namespaces the
# sandbox needs.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
export PATH="$ROOT/.tools/bin:$PATH"
if [[ -d "$ROOT/.tools/lib" ]]; then
  export LD_LIBRARY_PATH="$ROOT/.tools/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
fi
export PUPPETEER_CACHE_DIR="${PUPPETEER_CACHE_DIR:-$ROOT/.tools/puppeteer}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mapfile -t files < <(git ls-files --cached --others --exclude-standard '*.md' | sort)

total=0
failed=0
for f in "${files[@]}"; do
  [[ -f "$f" ]] || continue
  # Extract each block to its own file, named after its starting line.
  out="$tmp/$(echo "$f" | tr '/' '_')"
  mkdir -p "$out"
  awk -v dir="$out" '
    /^[[:space:]]*```mermaid[[:space:]]*$/ { inblock = 1; start = NR; file = dir "/" NR ".mmd"; next }
    inblock && /^[[:space:]]*```[[:space:]]*$/ { inblock = 0; close(file); next }
    inblock { print > file }
  ' "$f"
  for block in "$out"/*.mmd; do
    [[ -e "$block" ]] || continue
    total=$((total + 1))
    line="$(basename "$block" .mmd)"
    if ! mmdc -q -p "$ROOT/scripts/puppeteer-config.json" -i "$block" -o "${block%.mmd}.svg" >"$tmp/err" 2>&1; then
      echo "FAIL: $f:$line mermaid block does not render" >&2
      sed 's/^/      /' "$tmp/err" | head -20 >&2
      failed=$((failed + 1))
    fi
  done
done

echo "mermaid: $((total - failed))/$total diagrams rendered"
[[ "$failed" -eq 0 ]]
