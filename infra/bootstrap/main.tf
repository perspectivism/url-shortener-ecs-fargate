# Bootstrap (ADR-0016, ADR-0017): remote state storage and GitHub OIDC CI roles.

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = var.project
      Environment = "shared"
      ManagedBy   = "terraform"
      Repository  = var.repository
      Owner       = var.owner
      CostCenter  = "${var.project}-shared"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  account_id   = data.aws_caller_identity.current.account_id
  partition    = data.aws_partition.current.partition
  state_bucket = "${var.project}-tfstate-${local.account_id}"
  state_arn    = "arn:${local.partition}:s3:::${local.state_bucket}"
  github_url   = "token.actions.githubusercontent.com"
  oidc_arn     = var.create_oidc_provider ? aws_iam_openid_connect_provider.github[0].arn : data.aws_iam_openid_connect_provider.github[0].arn
  ci_role_arns = "arn:${local.partition}:iam::${local.account_id}:role/${var.project}-ci-*"
  boundary_arn = "arn:${local.partition}:iam::${local.account_id}:policy/${var.project}-ci-boundary"
}

# --- State storage ---------------------------------------------------------------

data "aws_iam_policy_document" "state_key" {
  #checkov:skip=CKV_AWS_109:Key policy root delegation; in a key policy "*" means this key (AWS-documented pattern).
  #checkov:skip=CKV_AWS_111:Key policy root delegation; in a key policy "*" means this key (AWS-documented pattern).
  #checkov:skip=CKV_AWS_356:Key policy resources must be "*" (this key); there is no narrower ARN.
  statement {
    sid       = "EnableIAMPolicies"
    effect    = "Allow"
    actions   = ["kms:*"]
    resources = ["*"] # In a key policy, "*" means this key.

    principals {
      type        = "AWS"
      identifiers = ["arn:${local.partition}:iam::${local.account_id}:root"]
    }
  }
}

resource "aws_kms_key" "state" {
  description             = "${var.project} Terraform state"
  enable_key_rotation     = true
  deletion_window_in_days = 7
  policy                  = data.aws_iam_policy_document.state_key.json
}

resource "aws_kms_alias" "state" {
  name          = "alias/${var.project}-tfstate"
  target_key_id = aws_kms_key.state.key_id
}

module "state_logs" {
  source = "../modules/log-bucket"

  name           = "${local.state_bucket}-logs"
  force_destroy  = var.force_destroy
  retention_days = 365
}

module "state_bucket" {
  source = "../modules/s3-bucket"

  name                               = local.state_bucket
  kms_key_arn                        = aws_kms_key.state.arn
  force_destroy                      = var.force_destroy
  access_log_bucket                  = module.state_logs.id
  noncurrent_version_expiration_days = 90 # state history for recovery
}

# --- GitHub OIDC -----------------------------------------------------------------

resource "aws_iam_openid_connect_provider" "github" {
  count = var.create_oidc_provider ? 1 : 0

  url            = "https://${local.github_url}"
  client_id_list = ["sts.amazonaws.com"]
}

data "aws_iam_openid_connect_provider" "github" {
  count = var.create_oidc_provider ? 0 : 1

  url = "https://${local.github_url}"
}

data "aws_iam_policy_document" "trust" {
  for_each = merge(
    { plan = "repo:${var.repository}:pull_request" },
    { deploy-plan = "repo:${var.repository}:ref:refs/heads/main" },
    { for env in var.environments : "apply-${env}" => "repo:${var.repository}:environment:${env}" },
  )

  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [local.oidc_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.github_url}:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.github_url}:sub"
      values   = [each.value]
    }
  }
}

# --- Permissions boundary for every CI role and every role CI creates -----------

locals {
  # Services this project's Terraform manages.
  managed_services = [
    "acm", "application-autoscaling", "athena", "budgets", "ce", "cloudfront", "cloudwatch",
    "dynamodb", "ec2", "ecr", "ecs", "elasticache", "elasticloadbalancing", "events", "glue",
    "iam", "kms", "logs", "route53", "scheduler", "secretsmanager", "sns", "sqs", "wafv2",
  ]
  # Every bucket this project manages is named <project>-*; S3 is limited to them.
  # What Terraform needs to manage (and force-destroy) buckets — not s3:*,
  # which would include e.g. s3:BypassGovernanceRetention.
  bucket_actions = ["s3:Create*", "s3:Delete*", "s3:Get*", "s3:List*", "s3:Put*"]
  project_buckets = [
    "arn:${local.partition}:s3:::${var.project}-*",
    "arn:${local.partition}:s3:::${var.project}-*/*",
  ]
}

data "aws_iam_policy_document" "boundary" {
  # Justification (M7 IAM review): this is the ceiling for a Terraform deploy
  # role, which must create, update and delete every resource type in the
  # listed services; ARNs do not exist before creation, so resources cannot be
  # enumerated. The Deny statements below carve out what must never happen
  # (long-lived credentials, unbounded roles, bootstrap and state tampering).
  statement {
    sid       = "ProjectServices"
    effect    = "Allow"
    actions   = concat([for s in local.managed_services : "${s}:*"], ["sts:GetCallerIdentity", "access-analyzer:ValidatePolicy", "ssmmessages:*"]) # service-level ceiling, see above
    resources = ["*"]                                                                                                                              # resources do not exist before apply
  }

  statement {
    sid       = "ProjectBuckets"
    effect    = "Allow"
    actions   = local.bucket_actions
    resources = local.project_buckets
  }

  statement {
    sid       = "NoLongLivedCredentials"
    effect    = "Deny"
    actions   = ["iam:CreateUser", "iam:CreateAccessKey", "iam:CreateLoginProfile", "iam:UpdateLoginProfile"]
    resources = ["*"]
  }

  statement {
    sid       = "RolesMustCarryThisBoundary"
    effect    = "Deny"
    actions   = ["iam:CreateRole", "iam:PutRolePermissionsBoundary"]
    resources = ["*"]

    condition {
      test     = "StringNotEquals"
      variable = "iam:PermissionsBoundary"
      values   = [local.boundary_arn]
    }
  }

  statement {
    sid       = "BoundaryPolicyIsImmutable"
    effect    = "Deny"
    actions   = ["iam:CreatePolicyVersion", "iam:DeletePolicy", "iam:DeletePolicyVersion", "iam:SetDefaultPolicyVersion", "iam:TagPolicy", "iam:UntagPolicy"]
    resources = [local.boundary_arn]
  }

  statement {
    sid       = "BoundaryNeverDetached"
    effect    = "Deny"
    actions   = ["iam:DeleteRolePermissionsBoundary"]
    resources = ["*"]
  }

  # Only this project's roles may be modified or passed to services.
  statement {
    sid           = "OnlyProjectRoles"
    effect        = "Deny"
    actions       = ["iam:AttachRolePolicy", "iam:PutRolePolicy", "iam:UpdateAssumeRolePolicy", "iam:PassRole"]
    not_resources = ["arn:${local.partition}:iam::${local.account_id}:role/${var.project}-*"]
  }

  statement {
    sid    = "BootstrapIsOffLimits"
    effect = "Deny"
    actions = [
      "iam:AttachRolePolicy", "iam:DeleteRole", "iam:DeleteRolePolicy", "iam:DetachRolePolicy",
      "iam:PutRolePolicy", "iam:UpdateAssumeRolePolicy", "iam:UpdateRole", "iam:TagRole", "iam:UntagRole",
      "iam:DeleteOpenIDConnectProvider", "iam:UpdateOpenIDConnectProviderThumbprint",
      "iam:AddClientIDToOpenIDConnectProvider", "iam:RemoveClientIDFromOpenIDConnectProvider",
    ]
    resources = [local.ci_role_arns, local.oidc_arn]
  }

  statement {
    sid       = "StateStorageIsOffLimits"
    effect    = "Deny"
    actions   = ["s3:DeleteBucket", "s3:PutBucketPolicy", "s3:DeleteBucketPolicy", "s3:PutBucketVersioning", "s3:PutLifecycleConfiguration", "s3:PutEncryptionConfiguration"]
    resources = [local.state_arn]
  }

  statement {
    sid       = "StateKeyIsOffLimits"
    effect    = "Deny"
    actions   = ["kms:ScheduleKeyDeletion", "kms:DisableKey", "kms:PutKeyPolicy", "kms:CreateGrant"]
    resources = [aws_kms_key.state.arn]
  }
}

resource "aws_iam_policy" "boundary" {
  name        = "${var.project}-ci-boundary"
  description = "Permissions boundary for CI roles and every role CI creates"
  policy      = data.aws_iam_policy_document.boundary.json
}

# --- Plan role (pull requests): read-only, no data-plane reads -------------------

data "aws_iam_policy" "read_only" {
  arn = "arn:${local.partition}:iam::aws:policy/ReadOnlyAccess"
}

data "aws_iam_policy_document" "plan" {
  statement {
    sid       = "ReadState"
    effect    = "Allow"
    actions   = ["s3:GetObject", "s3:ListBucket"]
    resources = [local.state_arn, "${local.state_arn}/*"]
  }

  # S3 native locking writes a .tflock object next to each state file.
  statement {
    sid       = "StateLockFiles"
    effect    = "Allow"
    actions   = ["s3:PutObject", "s3:DeleteObject"]
    resources = ["${local.state_arn}/*.tflock"]
  }

  statement {
    sid       = "StateKey"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [aws_kms_key.state.arn]
  }

  # ReadOnlyAccess includes some data reads; plans never need application data.
  statement {
    sid    = "NoApplicationData"
    effect = "Deny"
    actions = [
      "dynamodb:GetItem", "dynamodb:BatchGetItem", "dynamodb:Query", "dynamodb:Scan",
      "sqs:ReceiveMessage", "secretsmanager:GetSecretValue",
      "athena:GetQueryResults", "logs:GetLogEvents", "logs:FilterLogEvents", "logs:StartQuery",
    ]
    resources = ["*"]
  }

  statement {
    sid           = "NoObjectReadsOutsideState"
    effect        = "Deny"
    actions       = ["s3:GetObject", "s3:GetObjectVersion"]
    not_resources = ["${local.state_arn}/*"]
  }
}

resource "aws_iam_role" "plan" {
  name                 = "${var.project}-ci-plan"
  assume_role_policy   = data.aws_iam_policy_document.trust["plan"].json
  permissions_boundary = aws_iam_policy.boundary.arn
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "plan_read_only" {
  role       = aws_iam_role.plan.name
  policy_arn = data.aws_iam_policy.read_only.arn
}

resource "aws_iam_role_policy" "plan" {
  name   = "state-read"
  role   = aws_iam_role.plan.id
  policy = data.aws_iam_policy_document.plan.json
}

# --- Deploy plan role (deploy.yml plan jobs on main, ADR-0028, ADR-0032) --------
# The deploy workflow plans first, without approval, so reviewers approve the
# saved plan itself. The plan jobs run without a GitHub environment, so the
# role trusts the main branch (repo:<repo>:ref:refs/heads/main): only reviewed
# code runs there. Same read-only permissions as the PR plan role, plus
# writing saved plans under plans/ in the state bucket, where only the CI
# roles can read them; the apply job verifies its plan's SHA-256 from the same
# run, so a plan written by any other job is refused.

data "aws_iam_policy_document" "deploy_plan" {
  source_policy_documents = [data.aws_iam_policy_document.plan.json]

  statement {
    sid       = "SavedPlan"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${local.state_arn}/plans/*"]
  }
}

resource "aws_iam_role" "deploy_plan" {
  name                 = "${var.project}-ci-deploy-plan"
  assume_role_policy   = data.aws_iam_policy_document.trust["deploy-plan"].json
  permissions_boundary = aws_iam_policy.boundary.arn
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "deploy_plan_read_only" {
  role       = aws_iam_role.deploy_plan.name
  policy_arn = data.aws_iam_policy.read_only.arn
}

resource "aws_iam_role_policy" "deploy_plan" {
  name   = "state-read-plan-write"
  role   = aws_iam_role.deploy_plan.id
  policy = data.aws_iam_policy_document.deploy_plan.json
}

# --- Apply roles (GitHub environments dev / prod) ---------------------------------

data "aws_iam_policy_document" "apply" {
  for_each = var.environments

  # Justification (M7 IAM review): the apply role is Terraform's deploy role
  # and must manage every resource type in the project's services, before
  # their ARNs exist. It is bounded by the permissions boundary, the Deny
  # statements below (other environment's state and tagged resources), and
  # the GitHub OIDC trust (one repository environment per role).
  statement {
    sid       = "ProjectServices"
    effect    = "Allow"
    actions   = concat([for s in local.managed_services : "${s}:*"], ["sts:GetCallerIdentity", "access-analyzer:ValidatePolicy"]) # service-level, see above
    resources = ["*"]                                                                                                             # resources do not exist before apply
  }

  statement {
    sid       = "ProjectBuckets"
    effect    = "Allow"
    actions   = local.bucket_actions
    resources = local.project_buckets
  }

  statement {
    sid       = "StateKey"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [aws_kms_key.state.arn]
  }

  # Each environment may only touch its own state file and lock.
  statement {
    sid     = "OnlyOwnState"
    effect  = "Deny"
    actions = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = concat(
      ["${local.state_arn}/bootstrap/*"],
      [for e in var.environments : "${local.state_arn}/envs/${e}/*" if e != each.key],
    )
  }

  # Single-account guardrail (ADR-0019): never modify the other environment's resources.
  statement {
    sid       = "NotOtherEnvironments"
    effect    = "Deny"
    actions   = ["*"]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "aws:ResourceTag/Environment"
      values   = [for e in var.environments : e if e != each.key]
    }
  }
}

resource "aws_iam_role" "apply" {
  for_each = var.environments

  name                 = "${var.project}-ci-apply-${each.key}"
  assume_role_policy   = data.aws_iam_policy_document.trust["apply-${each.key}"].json
  permissions_boundary = aws_iam_policy.boundary.arn
  max_session_duration = 3600
}

resource "aws_iam_role_policy" "apply" {
  for_each = var.environments

  name   = "terraform-apply"
  role   = aws_iam_role.apply[each.key].id
  policy = data.aws_iam_policy_document.apply[each.key].json
}

# --- Cost allocation tag (ADR-0022) ------------------------------------------------
#
# Budgets and Cost Anomaly Detection filter each environment on CostCenter.
# Billing only lists a tag key after it has seen tagged usage (up to 24 h), so
# activation is a second step: apply once, then set activate_cost_allocation_tag.

resource "aws_ce_cost_allocation_tag" "cost_center" {
  count = var.activate_cost_allocation_tag ? 1 : 0

  tag_key = "CostCenter"
  status  = "Active"
}
