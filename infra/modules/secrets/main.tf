# Secret containers only (ADR-0014): Terraform knows names, ARNs, the CMK and
# the access policy — never values. Values are written out of band by
# scripts/seed-secrets.sh (make seed-secrets), so they never enter state.

locals {
  secrets = {
    safe_browsing_api_key = {
      name        = "${var.name_path}/safe-browsing-api-key"
      description = "Google Safe Browsing API key (consumers: api, nightly)"
    }
    visitor_hash_salt = {
      name        = "${var.name_path}/visitor-hash-salt"
      description = "HMAC salt for visitor IDs (consumer: redirect)"
    }
  }
}

resource "aws_secretsmanager_secret" "this" {
  #checkov:skip=CKV2_AWS_57:The Safe Browsing key is issued by Google (no rotation Lambda possible); rotating the visitor salt resets unique-visitor continuity, so both rotate via runbook procedures.
  for_each = local.secrets

  name                    = each.value.name
  description             = each.value.description
  kms_key_id              = var.kms_key_arn
  recovery_window_in_days = var.force_destroy ? 0 : 30
}

# Defense in depth on top of IAM: only the listed roles may read the value.
data "aws_iam_policy_document" "this" {
  for_each = local.secrets

  statement {
    sid       = "OnlyListedReaders"
    effect    = "Deny"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = ["*"] # In a secret resource policy, "*" means this secret.

    principals {
      type        = "AWS"
      identifiers = ["*"]
    }

    dynamic "condition" {
      for_each = length(var.readers[each.key]) > 0 ? [var.readers[each.key]] : []
      content {
        test     = "StringNotEquals"
        variable = "aws:PrincipalArn"
        values   = condition.value
      }
    }
  }
}

resource "aws_secretsmanager_secret_policy" "this" {
  for_each = local.secrets

  secret_arn          = aws_secretsmanager_secret.this[each.key].arn
  policy              = data.aws_iam_policy_document.this[each.key].json
  block_public_policy = true
}
