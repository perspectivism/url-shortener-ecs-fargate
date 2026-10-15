# ADR-0019: Single AWS account for dev/prod (multi-account as future)

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

AWS recommends separate accounts per environment (AWS Organizations, Control
Tower): the account is the strongest isolation boundary for IAM, quotas, cost
and blast radius. This project is a demo deployment run by one owner
in one existing account.

## Decision Drivers

- Simplicity for a single owner; one bootstrap, one set of credentials
- Still prevent dev automation from touching prod
- A credible path to multi-account

## Considered Options

1. One account, environments separated by naming, tags and IAM conditions
2. Two accounts (dev, prod) under AWS Organizations
3. Full landing zone (Control Tower: security, log-archive, workload accounts)

## Decision

Chosen option: **1 — one account**, with guardrails:

- Every resource is named `shortener-{env}-*` and tagged `Environment`.
- Separate apply roles per GitHub environment (`shortener-ci-apply-dev`,
  `shortener-ci-apply-prod`), trusted only for `repo:{owner}/{repo}:environment:{env}`.
- Each apply role **denies all actions on resources tagged with another
  environment** and denies access to the other environment's state file.
- Separate CMKs per environment; secrets and tables are environment-scoped by name.
- A shared permissions boundary stops CI from creating IAM users, access keys,
  or roles without the boundary, and from editing bootstrap resources.

## Consequences

### Positive

- Minimal setup; the whole system fits in one bootstrap.
- Guardrails make cross-environment changes by dev automation fail closed.

### Negative

- Tag-based isolation is weaker than an account boundary: untaggable resources,
  and actions that do not support `aws:ResourceTag` conditions, are not covered.
- Shared account-level quotas (e.g. Fargate vCPU, ENIs) and a shared bill.
- An account-level compromise affects both environments.

## Summary

The demo uses one account with per-environment roles that deny anything
tagged with the other environment; a production setup would move each environment
into its own account under Organizations, because the account is the only
hard isolation boundary in AWS.

## Future Work

- AWS Organizations with dev/prod (and log-archive/security) accounts, SCPs,
  and cross-account CI roles (SPEC §20).
