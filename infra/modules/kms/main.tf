# One customer-managed key per environment (SPEC §11.3) for DynamoDB, SQS, S3,
# CloudWatch Logs, Secrets Manager, ECR and ElastiCache. Access for workloads
# is granted through IAM policies (the key policy delegates to the account);
# only AWS services that cannot use IAM delegation get explicit statements.

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = data.aws_region.current.region
}

data "aws_iam_policy_document" "key" {
  #checkov:skip=CKV_AWS_109:Key policy root delegation; in a key policy "*" means this key (AWS-documented pattern).
  #checkov:skip=CKV_AWS_111:Key policy root delegation; in a key policy "*" means this key (AWS-documented pattern).
  #checkov:skip=CKV_AWS_356:Key policy resources must be "*" (this key); there is no narrower ARN.
  # Delegate key permissions to IAM in this account (standard root statement).
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

  # CloudWatch Logs encrypts log groups with the key directly (not via IAM).
  statement {
    sid    = "AllowCloudWatchLogs"
    effect = "Allow"
    actions = [
      "kms:Encrypt*",
      "kms:Decrypt*",
      "kms:ReEncrypt*",
      "kms:GenerateDataKey*",
      "kms:Describe*",
    ]
    resources = ["*"]

    principals {
      type        = "Service"
      identifiers = ["logs.${local.region}.amazonaws.com"]
    }

    condition {
      test     = "ArnLike"
      variable = "kms:EncryptionContext:aws:logs:arn"
      values   = [for p in var.log_group_name_prefixes : "arn:${local.partition}:logs:${local.region}:${local.account_id}:log-group:${p}*"]
    }
  }

  # Services that publish to the CMK-encrypted alarm topic (observability module).
  statement {
    sid       = "AllowAlertPublishersToEncryptedSNS"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey*"]
    resources = ["*"]

    principals {
      type = "Service"
      identifiers = [
        "cloudwatch.amazonaws.com",
        "budgets.amazonaws.com",
        "costalerts.amazonaws.com",
        "events.amazonaws.com",
      ]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }
}

resource "aws_kms_key" "this" {
  description             = "${var.name_prefix} data encryption key"
  enable_key_rotation     = true
  rotation_period_in_days = 365
  deletion_window_in_days = var.deletion_window_in_days
  policy                  = data.aws_iam_policy_document.key.json
}

resource "aws_kms_alias" "this" {
  name          = "alias/${var.name_prefix}"
  target_key_id = aws_kms_key.this.key_id
}
