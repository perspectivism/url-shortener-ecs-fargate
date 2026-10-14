"""Relative link and anchor checker for the repository's Markdown files.

Reads Markdown paths (one per line) on stdin. For every inline link or image
whose target is relative (not http/https/mailto), verifies the target file
exists and, if the link has a #fragment pointing into a Markdown file, that a
heading with that GitHub-style anchor exists. Exits non-zero on any failure.
"""

import os
import re
import sys
import unicodedata
from urllib.parse import unquote

LINK_RE = re.compile(r"!?\[(?:[^\[\]]|\[[^\]]*\])*\]\(\s*<?([^)\s>]+)>?(?:\s+\"[^\"]*\")?\s*\)")
HEADING_RE = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")
FENCE_RE = re.compile(r"^\s*(```|~~~)")
INLINE_CODE_RE = re.compile(r"`[^`]*`")
HTML_ANCHOR_RE = re.compile(r"<a\s+(?:name|id)=\"([^\"]+)\"")


def github_slug(text: str) -> str:
    """Approximates GitHub's heading anchor algorithm (github-slugger)."""
    text = re.sub(r"<[^>]+>", "", text)  # strip inline HTML
    text = re.sub(r"!?\[([^\]]*)\]\([^)]*\)", r"\1", text)  # links -> text
    text = text.replace("`", "").lower()
    out = []
    for ch in text:
        cat = unicodedata.category(ch)
        if ch in (" ", "-") or ch == "_" or cat[0] in ("L", "M", "N"):
            out.append(ch)
    return "".join(out).replace(" ", "-")


def outside_fences(lines):
    """Yields (lineno, text) for lines outside fenced code blocks."""
    in_fence = False
    for i, line in enumerate(lines, 1):
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if not in_fence:
            yield i, line


def strip_code(lines):
    """Yields (lineno, text) for lines outside fenced code blocks, inline code removed."""
    for i, line in outside_fences(lines):
        yield i, INLINE_CODE_RE.sub("", line)


def anchors_for(path, cache):
    if path not in cache:
        slugs = set()
        counts = {}
        with open(path, encoding="utf-8") as fh:
            # Headings keep inline code: GitHub slugs its text (backticks dropped).
            for _, line in outside_fences(fh.read().splitlines()):
                for m in HTML_ANCHOR_RE.finditer(INLINE_CODE_RE.sub("", line)):
                    slugs.add(m.group(1))
                h = HEADING_RE.match(line)
                if not h:
                    continue
                base = github_slug(h.group(2))
                n = counts.get(base, 0)
                counts[base] = n + 1
                slugs.add(base if n == 0 else f"{base}-{n}")
        cache[path] = slugs
    return cache[path]


def main() -> int:
    files = [line.strip() for line in sys.stdin if line.strip()]
    cache = {}
    errors = 0
    checked = 0
    for md in files:
        if not os.path.isfile(md):
            continue
        with open(md, encoding="utf-8") as fh:
            lines = fh.read().splitlines()
        for lineno, line in strip_code(lines):
            for m in LINK_RE.finditer(line):
                target = m.group(1)
                if re.match(r"^[a-z][a-z0-9+.-]*:", target, re.I):
                    continue  # absolute URL (http, https, mailto, ...)
                checked += 1
                path_part, _, frag = target.partition("#")
                path_part = unquote(path_part)
                if path_part:
                    resolved = os.path.normpath(os.path.join(os.path.dirname(md), path_part))
                    if not os.path.exists(resolved):
                        print(f"FAIL: {md}:{lineno}: broken link -> {target}", file=sys.stderr)
                        errors += 1
                        continue
                else:
                    resolved = md
                if frag and resolved.endswith(".md") and os.path.isfile(resolved):
                    if unquote(frag) not in anchors_for(resolved, cache):
                        print(f"FAIL: {md}:{lineno}: missing anchor #{frag} in {resolved}", file=sys.stderr)
                        errors += 1
    print(f"links: {checked - errors}/{checked} relative links resolved across {len(files)} files")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
