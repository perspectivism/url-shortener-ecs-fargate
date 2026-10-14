#!/usr/bin/env bash
# Checks every relative link (and #anchor) in every Markdown file resolves.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
git ls-files --cached --others --exclude-standard '*.md' | python3 -I scripts/check_links.py
