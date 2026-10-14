# ADR-0023: IAM review, VPC endpoint data perimeter and WAF tuning

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

M7 (SPEC §18) hardens what M4–M6 built. Three requirements apply:

- every IAM policy is validated with IAM Access Analyzer;
- there is no `"Action": "*"` or `"Resource": "*"` unless AWS requires it,
  and each such case is justified inline;
- the threat model must cover data exfiltration through endpoint policies and KMS (SPEC §17.3).

The review found three gaps:

1. The interface endpoints (ECR, CloudWatch Logs, SQS, Secrets Manager) had
   the default full-access policy. Code running in a task could have used them
   with **another account's** credentials or resources, for example by sending
   data to an attacker's queue or log group.
2. The S3 gateway endpoint allowed `s3:*` on any bucket in this account. That
   is wider than the environment's own buckets.
3. The WAF rate rules ran after the managed rule groups. A rate-based rule
   only counts requests that reach it, so requests blocked earlier never
   counted toward the per-IP limits.

## Decision Drivers

- Contain a compromised task: it must not be able to move data out of the account
- Least privilege is checked by tools, not by reviewer memory
- Nothing that cannot be verified offline may break deploys silently

## Considered Options

1. Rely on task-role IAM policies alone (status quo)
2. **Endpoint data perimeter** (identity + resource account) plus automated IAM checks
3. Full organization-level perimeter (SCPs/RCPs with `aws:ResourceOrgID`)

## Decision

Chosen option: **2**.

- **Interface endpoints** each get a policy limited to their own service's
  actions (`ecr:*`, `logs:*`, `sqs:*`, `secretsmanager:*`). It allows requests only when
  `aws:PrincipalAccount` **and** `aws:ResourceAccount` are this account. These
  keys are present for almost every action; AWS documents only a short list of
  exceptions, none of which we use (checked 2026-10-14). The one exception is
  `ssmmessages`, used only for break-glass ECS Exec, which has no resource ARNs.
- **S3 gateway endpoint:** `GetObject`, `PutObject` and
  `AbortMultipartUpload` on `<project>-<env>-*` buckets in this account,
  plus `GetObject` on the AWS-owned ECR layer bucket. **DynamoDB gateway
  endpoint:** this environment's tables only.
- **IAM wildcards:** `scripts/check_iam_wildcards.py` (in `make tf-check`)
  fails on any Allow statement with `*`, `service:*`, resource `*` or principal
  `*` that lacks an inline justification. The remaining cases are:

  | Case | Why it is needed |
  | --- | --- |
  | Key policies | `*` means the key itself |
  | Endpoint policies | they only narrow access; a principal `*` is required |
  | `ecr:GetAuthorizationToken` and `ssmmessages` | no resource-level permissions |
  | CI deploy role and its boundary | service-level actions on `*`: Terraform must manage resources before their ARNs exist; bounded by Deny statements |

- **Access Analyzer:** `scripts/validate-iam.sh` (`make validate-iam`) runs
  `validate-policy` on every rendered policy in a plan or state, with the right
  policy type and resource type. It fails on `ERROR` or `SECURITY_WARNING`
  findings, and on any policy it could not render.
- **WAF:** the rate rules move to priorities 1–2, ahead of every managed group.
  `GenericRFI_BODY` keeps blocking request bodies that contain URLs with IPv4
  literals; those are phishing indicators, and the trade-off is accepted in
  the threat model (I9).
- **Suppressions** are checked against the threat model
  (`scripts/check_suppressions.py`), and every STRIDE row must be mitigated or
  accepted (`scripts/check_threat_model.py`).

## Consequences

### Positive

- A compromised task cannot exfiltrate through AWS APIs to another account,
  even with attacker credentials, and it has no internet egress (except
  Safe Browsing via NAT when scanning is on).
- Least-privilege regressions fail the build offline; Access Analyzer adds
  AWS's own policy grammar and security checks.

### Negative

- The endpoint policies cannot be exercised offline. The first deploy
  verifies them: services reach steady state, which requires ECR, Logs and
  Secrets Manager.
- Access Analyzer needs AWS credentials and rendered policies. Against an
  empty-state plan most policies are unknown, so the full run happens after
  the first apply (pending cloud check).
- A new AWS-owned resource dependency (like ECR's layer bucket) would need an
  explicit exception.

## Interview Angle

> "Task roles say what a service *may* do. The VPC endpoint policies also
> say *whose* resources it may touch. Even with stolen credentials from another
> account, a compromised container can't push data out through our
> endpoints. Every wildcard in IAM is justified in a comment, and CI fails
> on one that isn't."

## Future Work

- Organization-level data perimeter (RCPs/SCPs with `aws:ResourceOrgID`)
  once the project moves to multi-account (ADR-0019).
- IAM Access Analyzer unused-access findings and `check-no-new-access` in CI
  for policy diffs.
