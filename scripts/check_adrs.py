#!/usr/bin/env python3
"""ADR completeness (SPEC §17.2, M9 gate).

Every required ADR (0001-0020) exists; every ADR has status Accepted or
"Superseded by" a linked ADR, the MADR sections plus "Summary", and a
row in docs/adr/README.md.
"""

import pathlib
import re
import sys

ADR = pathlib.Path("docs/adr")
REQUIRED = [f"{n:04d}" for n in range(1, 21)]
SECTIONS = ["## Context", "## Decision Drivers", "## Considered Options", "## Decision", "## Consequences", "## Summary"]

files = sorted(p for p in ADR.glob("[0-9][0-9][0-9][0-9]-*.md"))
index = (ADR / "README.md").read_text()
numbers = {p.name[:4] for p in files}
problems = [f"missing required ADR {n}" for n in REQUIRED if n not in numbers]
for p in files:
    text = p.read_text()
    status = re.search(r"^- \*\*Status:\*\* (.+)$", text, re.M)
    if not status:
        problems.append(f"{p}: no Status line")
    elif not (status.group(1).startswith("Accepted") or re.match(r"Superseded by \[ADR-\d{4}\]\(\d{4}-[^)]+\.md\)", status.group(1))):
        problems.append(f"{p}: status {status.group(1)!r} (want Accepted or Superseded by [ADR-NNNN](link))")
    for sec in SECTIONS:
        if not re.search(rf"^{re.escape(sec)}", text, re.M):
            problems.append(f"{p}: missing section {sec!r}")
    if f"]({p.name})" not in index:
        problems.append(f"{p}: not listed in docs/adr/README.md")
for p in problems:
    print(p, file=sys.stderr)
if problems:
    print(f"FAIL: {len(problems)} ADR problem(s)", file=sys.stderr)
    sys.exit(1)
print(f"ADRs OK: {len(files)} ADRs (all 20 required present), all Accepted, MADR sections + Summary, all indexed")
