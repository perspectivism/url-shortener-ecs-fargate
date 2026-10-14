# Per-environment log archive: S3 server access logs (prefix s3/), ALB access
# logs (prefix alb/) and CloudFront standard logs (prefix cloudfront/).
# Encryption is SSE-S3 because S3 server access logging and ALB log delivery
# do not support SSE-KMS target buckets (verified 2026-10-14, see ADR-0013).

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = data.aws_region.current.region
  arn        = "arn:${local.partition}:s3:::${var.name}"
}

data "aws_iam_policy_document" "delivery" {
  statement {
    sid       = "S3ServerAccessLogs"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${local.arn}/s3/*"]

    principals {
      type        = "Service"
      identifiers = ["logging.s3.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }

  statement {
    sid       = "ALBAccessLogs"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${local.arn}/alb/AWSLogs/${local.account_id}/*"]

    principals {
      type        = "Service"
      identifiers = ["logdelivery.elasticloadbalancing.amazonaws.com"]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:${local.partition}:elasticloadbalancing:${local.region}:${local.account_id}:loadbalancer/*"]
    }
  }

  statement {
    sid       = "CloudFrontStandardLogs"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${local.arn}/cloudfront/*"]

    principals {
      type        = "Service"
      identifiers = ["delivery.logs.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }
}

module "bucket" {
  source = "../s3-bucket"

  name                               = var.name
  kms_key_arn                        = null
  force_destroy                      = var.force_destroy
  access_log_bucket                  = null # never log the log bucket into itself
  expiration_days                    = var.retention_days
  noncurrent_version_expiration_days = 1
  extra_policy_json                  = data.aws_iam_policy_document.delivery.json
}
