#!/usr/bin/env python3
"""Every ADR mention in the docs must be a link (docs-check).

Finds "ADR-NNNN" outside links, fenced code, inline code and generated
terraform-docs blocks (Terraform descriptions cannot hold Markdown links). An
ADR's own title and references to itself are exempt.

Usage: check_adr_links.py [--fix]   (from the repository root)
  --fix rewrites bare mentions as [ADR-NNNN](relative/path.md)
"""

import os
import pathlib
import re
import subprocess
import sys

ADR_DIR = pathlib.Path("docs/adr")
SKIP = {"SPEC.md", "AGENTS.md"}
MENTION = re.compile(r"ADR-(\d{4})")
PROTECTED = re.compile(r"\[[^\]]*\]\([^)]*\)|`[^`]*`")  # links and inline code


def adr_files():
    return {p.name[:4]: p for p in ADR_DIR.glob("[0-9][0-9][0-9][0-9]-*.md")}


def markdown_files():
    out = subprocess.run(["git", "ls-files", "-co", "--exclude-standard", "*.md"], capture_output=True, text=True, check=True).stdout
    return [pathlib.Path(f) for f in out.split() if pathlib.Path(f).name not in SKIP]


def process(path, adrs, fix):
    own = path.name[:4] if path.parent == ADR_DIR and path.name[:4].isdigit() else None
    lines = path.read_text().split("\n")
    problems, out = [], []
    fence = generated = False
    for n, line in enumerate(lines, 1):
        stripped = line.strip()
        if "<!-- BEGIN_TF_DOCS -->" in line:
            generated = True
        if "<!-- END_TF_DOCS -->" in line:
            generated = False
        if stripped.startswith("```"):
            fence = not fence
        if fence or generated or stripped.startswith("```") or (own and n == 1):
            out.append(line)
            continue
        pieces, last = [], 0
        for m in PROTECTED.finditer(line):
            pieces.append((line[last:m.start()], True))
            pieces.append((m.group(0), False))
            last = m.end()
        pieces.append((line[last:], True))
        new = []
        for text, plain in pieces:
            if plain:
                def link(m):
                    num = m.group(1)
                    if num == own or num not in adrs:
                        return m.group(0)
                    problems.append(f"{path}:{n}: ADR-{num} is not a link")
                    rel = os.path.relpath(adrs[num], path.parent)
                    return f"[ADR-{num}]({rel})"
                text = MENTION.sub(link, text)
            new.append(text)
        out.append("".join(new))
    if fix and problems:
        path.write_text("\n".join(out))
    return problems


def main():
    fix = "--fix" in sys.argv[1:]
    adrs = adr_files()
    problems = []
    for p in markdown_files():
        problems += process(p, adrs, fix)
    if fix:
        print(f"linked {len(problems)} ADR mentions")
        return 0
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"FAIL: {len(problems)} ADR mentions are not links (run scripts/check_adr_links.py --fix)", file=sys.stderr)
        return 1
    print("ADR links OK: every ADR mention is a link")
    return 0


if __name__ == "__main__":
    sys.exit(main())
