#!/usr/bin/env python3
"""Offline IAM wildcard review over every aws_iam_policy_document (M7 gate).

Every *Allow* statement whose actions contain "*" or "service:*", whose
resources contain "*", or whose principals are "*" must carry an inline
justification: a comment on that attribute's line, directly above it, or
directly above the statement. Deny statements may use wildcards freely
(they only take permissions away).

Usage: check_iam_wildcards.py [root | file]   (default: infra)
"""

import pathlib
import re
import sys

WILD_ACTION = re.compile(r'"(\*|[a-z0-9-]+:\*|\$\{[^}]*\}:\*)"')
WILD_RESOURCE = re.compile(r'"\*"')


def blocks(text, opener):
    """Yield (start_offset, end_offset) of each `opener {...}` block, honouring strings and comments."""
    for m in re.finditer(opener, text):
        i = text.index("{", m.start())
        depth, j = 0, i
        in_str, interp = False, 0
        while j < len(text):
            c = text[j]
            if in_str:
                if c == "\\":
                    j += 2
                    continue
                if text.startswith("${", j):
                    interp += 1
                    j += 2
                    continue
                if interp and c == "}":
                    interp -= 1
                elif c == '"' and not interp:
                    in_str = False
            else:
                if c == "#" or text.startswith("//", j):
                    j = text.find("\n", j)
                    if j < 0:
                        break
                    continue
                if c == '"':
                    in_str = True
                elif c == "{":
                    depth += 1
                elif c == "}":
                    depth -= 1
                    if depth == 0:
                        yield m.start(), j + 1
                        break
            j += 1


def justified(lines, idx, stmt_line):
    """A comment on line idx, directly above it, or directly above the statement."""
    if "#" in lines[idx]:
        return True
    for k in (idx - 1, stmt_line - 1):
        if k >= 0 and lines[k].strip().startswith("#"):
            return True
    return False


def attribute(lines, start, end, name):
    """(line index, value text) of `name = ...` (a possibly multi-line list) inside lines[start:end]."""
    pat = re.compile(rf"^\s*{name}\s*=")
    for i in range(start, end):
        if pat.match(lines[i]):
            text, depth, k = "", 0, i
            while k < end:
                text += lines[k].split("#", 1)[0] + "\n"
                depth += text.count("[") + text.count("(") - text.count("]") - text.count(")") - depth
                if depth <= 0:
                    break
                k += 1
            return i, text
    return None, ""


def main():
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "infra")
    problems, checked = [], 0
    paths = [root] if root.is_file() else sorted(root.rglob("*.tf"))
    for path in paths:
        if ".terraform" in path.parts:
            continue
        text = path.read_text()
        for ds, de in blocks(text, r'data\s+"aws_iam_policy_document"\s+"[^"]+"\s*\{'):
            doc = text[ds:de]
            for ss, se in blocks(doc, r"(?m)^\s*statement\s*\{"):
                checked += 1
                abs_start = text.count("\n", 0, ds + ss)
                abs_end = text.count("\n", 0, ds + se) + 1
                lines = text.split("\n")
                body = "\n".join(lines[abs_start:abs_end])
                if re.search(r'effect\s*=\s*"Deny"', body):
                    continue
                checks = [("actions", WILD_ACTION, "wildcard action"), ("resources", WILD_RESOURCE, 'resource "*"')]
                for name, pat, what in checks:
                    i, value = attribute(lines, abs_start, abs_end, name)
                    if i is not None and pat.search(value) and not justified(lines, i, abs_start):
                        problems.append(f"{path}:{i + 1}: Allow statement with {what} and no inline justification")
                for i in range(abs_start, abs_end):
                    if re.match(r'^\s*identifiers\s*=\s*\["\*"\]', lines[i]):
                        t = next((k for k in range(i, abs_start, -1) if re.match(r"^\s*principals\s*\{", lines[k])), i)
                        if not (justified(lines, i, abs_start) or justified(lines, t, abs_start)):
                            problems.append(f'{path}:{i + 1}: Allow statement with principal "*" and no inline justification')
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"FAIL: {len(problems)} unjustified IAM wildcard(s) in {checked} statements", file=sys.stderr)
        return 1
    print(f"IAM wildcards OK: {checked} policy statements; every Allow wildcard is justified inline")
    return 0


if __name__ == "__main__":
    sys.exit(main())
