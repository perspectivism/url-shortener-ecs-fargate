#!/usr/bin/env python3
"""Every STRIDE row in docs/threat-model.md must be Mitigated or Accepted (M7 gate).

STRIDE rows are table rows whose first cell is an ID such as R1, A12 or C3; the
last cell is the status.
"""

import pathlib
import re
import sys

rows = [line for line in pathlib.Path("docs/threat-model.md").read_text().splitlines() if re.match(r"^\| [A-Z]\d+ \|", line)]
bad = []
for line in rows:
    status = line.rstrip().rstrip("|").rsplit("|", 1)[-1].strip()
    if not re.match(r"^(Mitigated|Accepted)\b", status):
        bad.append(f"{line.split('|')[1].strip()}: status {status!r}")
for b in bad:
    print(b, file=sys.stderr)
if bad or not rows:
    print(f"FAIL: {len(bad)} of {len(rows)} STRIDE rows are not mitigated/accepted", file=sys.stderr)
    sys.exit(1)
print(f"threat model OK: {len(rows)} STRIDE rows, all mitigated or accepted")
