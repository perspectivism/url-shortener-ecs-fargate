# ADR-0027: Manual approval for every deploy

- **Status:** Accepted (supersedes the automatic dev deploy of [ADR-0017](0017-cicd-github-oidc-rolling-deploys.md)); job layout superseded by [ADR-0028](0028-approve-the-saved-plan-versioned-environment-settings.md): reviewers now approve the saved plan
- **Date:** 2026-10-15
- **Deciders:** project owner, coding agent

## Context

SPEC §14 and [ADR-0017](0017-cicd-github-oidc-rolling-deploys.md) deployed
every merge to `main` straight to dev, with a reviewer gate only before prod.
The project owner decided that **no deployment happens automatically
without approval**, in any environment. There was a practical reason too:
before the AWS setup is done, the first push to GitHub would start a deploy
that fails at the AWS login step.

## Decision Drivers

- Every change to AWS resources is approved by a human (AGENTS.md §4, applied to CI)
- The rule is enforced in code, not only in repository settings that can drift
- Usable by a single maintainer

## Considered Options

1. Status quo: merge deploys dev; prod needs a reviewer
2. A repository switch (`DEPLOY_ENABLED`) that turns automatic deploys on after setup
3. **Manual trigger only, plus required reviewers on both environments**

## Decision

Chosen option: **3**.

- **Trigger:** `deploy.yml` has only `workflow_dispatch`. A human starts it
  (Actions → deploy → Run workflow, or `gh workflow run deploy --ref main`).
  There is no push, merge or schedule trigger, and the build job refuses any
  ref other than `main`.
- **Approval per environment:** the `dev` and `prod` jobs run in GitHub
  environments with required reviewers. A job cannot assume its AWS role
  until a reviewer approves it.
- **Enforced:** `scripts/check-deploy-gate.sh` (in `make check-workflows` and
  CI) fails when the workflow gains any other trigger, or when a job requests
  an OIDC token (`id-token: write`) outside the `dev`/`prod` environments.
- **Single maintainer:** "Prevent self-review" stays off until a second
  reviewer exists; otherwise the owner could never approve their own run
  ([runbook §5.1](../runbook.md#51-cicd-setup-one-time--repository-admin)).

Option 2 still deploys automatically once enabled. Option 1 is the CD default,
but it puts every merged change in dev without a deliberate decision.

**Deviation recorded:** SPEC §14 ("deploy.yml (main): build & push, apply dev, …").

## Consequences

### Positive

- No AWS change happens without two human acts: starting the run and approving the environment.
- Day-one pushes to GitHub run CI only, with no failed deploys before AWS is set up.
- The gate is checked by CI, so it cannot silently disappear.

### Negative

- Dev lags `main` until someone deploys; merges no longer give fast feedback in dev.
- An extra click per environment for every deploy.
- Branch protection and the environments' reviewer settings remain manual
  GitHub configuration (runbook §5.1); the check covers the workflow file only.

## Summary

Merges run CI, never deploys. A human starts each deploy and approves each
environment, and a CI check fails if anyone adds an automatic trigger or
lets a job assume an AWS role outside an approval-gated environment.
