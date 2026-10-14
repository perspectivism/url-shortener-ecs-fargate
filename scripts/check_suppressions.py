#!/usr/bin/env python3
"""Every scanner suppression must be listed in the threat model (AGENTS.md §3).

Finds checkov skips, Trivy ignores, Go nolint directives, tflint ignores,
shellcheck disables, gitleaks:allow markers, .trivyignore entries and gitleaks
allowlist paths, and requires a row in the threat model's "Scanner suppressions"
section that names the rule (or tool) and the file (`{a,b}` alternatives in a
path are expanded).

Usage: check_suppressions.py   (from the repository root)
"""

import itertools
import pathlib
import re
import subprocess
import sys

MODEL = pathlib.Path("docs/threat-model.md")
PATTERNS = [
    ("checkov", re.compile(r"#\s*checkov:skip=([A-Z0-9_]+)")),
    ("trivy", re.compile(r"#\s*trivy:ignore:([A-Za-z0-9-]+)")),
    ("golangci-lint", re.compile(r"//\s*nolint:([a-z0-9,]+)")),
    ("tflint", re.compile(r"#\s*tflint-ignore:\s*([a-z0-9_]+)")),
    ("shellcheck", re.compile(r"#\s*shellcheck\s+disable=(SC\d+(?:,SC\d+)*)")),
    ("gitleaks", re.compile(r"(gitleaks:allow)")),
]
SKIP_FILES = {"docs/threat-model.md", "SPEC.md", "AGENTS.md", "scripts/check_suppressions.py"}


def tracked_files():
    out = subprocess.run(["git", "ls-files", "-co", "--exclude-standard"], capture_output=True, text=True, check=True)
    return [f for f in out.stdout.splitlines() if f not in SKIP_FILES and not f.startswith("scripts/testdata/")]


def expand(path):
    """infra/modules/{a,b}/main.tf -> both paths."""
    parts = re.split(r"\{([^}]*)\}", path)
    options = [[p] if i % 2 == 0 else p.split(",") for i, p in enumerate(parts)]
    return {"".join(c) for c in itertools.product(*options)}


def table_rows():
    text = MODEL.read_text()
    section = text[re.search(r"^## \d+\. Scanner suppressions", text, re.M).start():]
    rows = []
    for line in section.splitlines():
        if line.startswith("| ") and not line.startswith("| ---") and not line.startswith("| Tool"):
            paths = set()
            for p in re.findall(r"`([^`]+)`", line):
                paths |= expand(p)
            rows.append((line, paths))
    return rows


def main():
    found = []
    for f in tracked_files():
        p = pathlib.Path(f)
        if not p.is_file() or p.suffix in {".png", ".svg", ".ico"}:
            continue
        try:
            text = p.read_text()
        except UnicodeDecodeError:
            continue
        for n, line in enumerate(text.splitlines(), 1):
            for tool, pat in PATTERNS:
                for m in pat.finditer(line):
                    for rule in re.split(r"[,]", m.group(1)):
                        found.append((tool, rule, f, n))
    if pathlib.Path(".trivyignore").exists():
        for n, line in enumerate(pathlib.Path(".trivyignore").read_text().splitlines(), 1):
            if line.strip() and not line.startswith("#"):
                found.append(("trivy", line.strip(), ".trivyignore", n))
    for m in re.finditer(r"paths\s*=\s*\[([^\]]*)\]", pathlib.Path(".gitleaks.toml").read_text()):
        for path in re.findall(r"'''([^']+)'''|\"([^\"]+)\"", m.group(1)):
            found.append(("gitleaks", (path[0] or path[1]).replace("\\\\", "\\"), ".gitleaks.toml", 0))

    rows = table_rows()
    missing = []
    for tool, rule, f, n in found:
        ok = any(rule.replace("\\", "") in line.replace("\\", "") and (f in paths or any(f.startswith(p.rstrip("/") + "/") for p in paths))
                 for line, paths in rows)
        if not ok:
            missing.append(f"{f}:{n}: {tool} {rule} has no row in {MODEL} (Scanner suppressions)")
    for m in missing:
        print(m, file=sys.stderr)
    if missing:
        print(f"FAIL: {len(missing)} of {len(found)} suppressions are not in the threat model", file=sys.stderr)
        return 1
    print(f"suppressions OK: {len(found)} suppressions, each listed in the threat model with its file")
    return 0


if __name__ == "__main__":
    sys.exit(main())
