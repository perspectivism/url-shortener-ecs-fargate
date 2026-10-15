#!/usr/bin/env python3
"""Counts quoted in the docs must match the repository (docs-check).

Each fact is derived from its source of truth and compared with every place
the docs state it:
  ADRs              files in docs/adr
  integration tests func Test* in tests/integration
  alarms            alarm names in runbook "### 4.n" headings (a Terraform test
                    keeps those equal to the alarms Terraform defines)
  STRIDE rows       rows in docs/threat-model.md
  terraform tests   run blocks in infra/**/*.tftest.hcl
"""

import pathlib
import re
import sys

WORDS = {"twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16}
DOCS = ["README.md", "docs/design.md", "docs/cost.md", "docs/runbook.md", "docs/threat-model.md"]


def num(s):
    return int(s) if s.isdigit() else WORDS.get(s.lower(), -1)


def main():
    runbook = pathlib.Path("docs/runbook.md").read_text()
    alarms = len({m for h in re.findall(r"(?m)^### 4\.\d+ (.*)$", runbook) for m in re.findall(r"`([a-z0-9-]+)`", h)})
    facts = {
        "ADRs": (len(list(pathlib.Path("docs/adr").glob("[0-9][0-9][0-9][0-9]-*.md"))),
                 [r"(\d+) architecture decision records", r"(\d+) ADRs"]),
        "integration tests": (sum(len(re.findall(r"(?m)^func Test", p.read_text())) for p in pathlib.Path("tests/integration").glob("*_test.go")),
                              [r"(\d+) end-to-end flows", r"(\d+) integration tests"]),
        "alarms": (alarms, [r"(\d+|[Ff]ourteen|[Ff]ifteen|[Tt]hirteen) alarms", r"[Aa]larms:\*\* (\d+) per environment", r"(\d+) alarms per environment"]),
        "STRIDE rows": (len(re.findall(r"(?m)^\| [A-Z]\d+ \|", pathlib.Path("docs/threat-model.md").read_text())),
                        [r"(\d+) STRIDE rows", r"(\d+) rows, all mitigated"]),
        "terraform test runs": (sum(len(re.findall(r'(?m)^run "', p.read_text())) for p in pathlib.Path("infra").rglob("*.tftest.hcl") if ".terraform" not in p.parts),
                                [r"(\d+) `terraform test` runs"]),
    }
    problems, checked = [], 0
    for doc in DOCS:
        text = pathlib.Path(doc).read_text()
        for name, (actual, patterns) in facts.items():
            for pat in patterns:
                for m in re.finditer(pat, text):
                    checked += 1
                    if num(m.group(1)) != actual:
                        line = text.count("\n", 0, m.start()) + 1
                        problems.append(f"{doc}:{line}: says {m.group(0)!r}, actual {name}: {actual}")
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"FAIL: {len(problems)} stale count(s) in the docs", file=sys.stderr)
        return 1
    print(f"doc facts OK: {checked} quoted counts match ({', '.join(f'{k} {v[0]}' for k, v in facts.items())})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
