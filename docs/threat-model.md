# 🛡️ URL Shortener — Threat Model

> **Status:** skeleton (M0). v0 (application DFD + STRIDE for API and redirect)
> lands in M1; complete per SPEC §17.3 in M7.

## Contents

1. [Scope](#1-scope)
2. [Scanner suppressions](#2-scanner-suppressions)

## 1. Scope

To be written in M1.

## 2. Scanner suppressions

Every scanner suppression in the repository (`nolint`, `#checkov:skip`,
`.trivyignore`, tflint ignores, gitleaks allowlists) must have a row here and an
inline justification (AGENTS.md §3).

| Tool | Location | Rule | Justification |
| --- | --- | --- | --- |
| gitleaks | `.gitleaks.toml` | path allowlist `^\.tools/` | Locally installed third-party toolchains (Go stdlib test fixtures, headless Chrome). Gitignored and never committed; only `detect --no-git` reaches it because it does not honour `.gitignore`. Git-history scans are unaffected. |
