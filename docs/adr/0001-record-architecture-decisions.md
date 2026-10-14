# ADR-0001: Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

The URL Shortener exists to demonstrate production-grade AWS container
architecture and to serve as a walkthrough for the "design a URL shortener"
interview (SPEC §1). Every significant decision has to be explainable later:
what we chose, what we rejected, and why. Decisions made in chat or commit
messages get lost; decisions made silently cannot be defended in an interview.

## Decision Drivers

- Decisions must be reviewable alongside the code that implements them.
- Each decision should map to an interview talking point.
- Records must be immutable once accepted, so history stays honest.

## Considered Options

1. No formal records (commit messages and README only)
2. A single living design document
3. Architecture Decision Records in MADR format, plus a design document

## Decision

Chosen option: **3 — ADRs in MADR format** stored in `docs/adr/NNNN-kebab-title.md`,
indexed in [`docs/adr/README.md`](README.md), using [`template.md`](template.md).

Project-specific rules:

- Each ADR has the MADR sections (Status, Date, Context, Decision Drivers,
  Considered Options, Decision, Consequences) plus an **Interview Angle**
  section: the decision explained in one breath.
- An ADR is written in the milestone where the decision is made, not at the end.
- Accepted ADRs are never edited in substance; a changed decision gets a new ADR
  that supersedes the old one (the old one's status links to its successor).
- Deviations from SPEC.md are recorded both in the relevant ADR and in
  `GATES.md`.
- `docs/design.md` tells the end-to-end story and links to ADRs for the "why".

## Consequences

### Positive

- The "why" lives next to the code and is versioned with it.
- The README interview walkthrough can link each step to its ADR.
- Supersession keeps an honest trail of changed minds.

### Negative

- Writing overhead for every significant decision.
- The index must be kept in sync (checked by the docs link check).

## Interview Angle

> "Every design decision is an ADR with the options I rejected and why, so I can
> defend any choice in the system, and the trail shows when I changed my mind."
