# ADR-0032: Two GitHub environments; deploy plans trust main

- **Status:** Accepted (supersedes the `dev-plan` / `prod-plan` environments and per-environment plan roles of [ADR-0028](0028-approve-the-saved-plan-versioned-environment-settings.md))
- **Date:** 2026-10-15
- **Deciders:** project owner, coding agent

## Context

[ADR-0028](0028-approve-the-saved-plan-versioned-environment-settings.md) split
each deploy into a plan job and an approval-gated apply job. It gave each plan
job its own GitHub environment (`dev-plan`, `prod-plan`, no reviewers) only to
get a distinct OIDC subject for a per-environment read-only role. The project
owner judged four environments too many for a single-maintainer project, and
the extra environments added nothing a reviewer approves.

## Decision Drivers

- As few moving parts as the guarantees allow
- Plan jobs stay read-only and cannot reach the apply roles
- Reviewers still approve the exact saved plan that is applied

## Considered Options

1. Keep four environments (two of them without reviewers)
2. **Two environments; plan jobs run without an environment under one read-only role that trusts the `main` branch**
3. Run plan jobs under the PR plan role (it trusts `pull_request` only; would need a second subject)

## Decision

Chosen option: **2**.

- **Environments:** only `dev` and `prod`, both with required reviewers.
- **Plan jobs** run without an environment. Their OIDC subject is
  `repo:<repo>:ref:refs/heads/main`, which the new role
  `shortener-ci-deploy-plan` trusts, exactly and nothing else. The role has
  the PR plan role's read-only permissions plus `s3:PutObject` on `plans/*`
  in the state bucket.
- **Variables:** `AWS_DEPLOY_PLAN_ROLE_ARN` and `ALERT_EMAIL` become
  repository variables.
- **Enforced:** `scripts/check-deploy-gate.sh` allows no environments other
  than `dev` and `prod`. A job without an environment that assumes a role must
  use the deploy plan role and must never push, apply or record an approval.

## Consequences

### Positive

- Two environments to configure instead of four, and one plan role instead of two.
- Approval semantics are unchanged: plan, read, approve, apply that plan.

### Negative

- Any job running on `main` that requests an OIDC token can assume the
  read-only deploy plan role, not only the deploy workflow's plan jobs. This
  is accepted:
  - code reaches `main` only through reviewed merges;
  - the role cannot change resources;
  - a plan it writes is refused at apply time, because the apply job checks
    the SHA-256 from its own run.
- A plan job can write to `plans/dev/` as well as `plans/prod/`. The hash
  check makes that harmless.

## Interview Angle

> "Environments are for approvals. The plan step needs an identity, not an
> approval, so it runs on main under a read-only role, and only the two
> approval-gated environments can apply."
