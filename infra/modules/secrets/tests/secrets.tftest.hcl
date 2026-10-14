mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_resource "aws_secretsmanager_secret" {
    defaults = { arn = "arn:aws:secretsmanager:us-east-1:123456789012:secret:shortener/test/x-AbCdEf" }
  }
}

variables {
  name_path     = "shortener/test"
  kms_key_arn   = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  force_destroy = true
}

run "containers_only_cmk_and_nobody_reads_by_default" {
  command = plan

  assert {
    condition     = toset([for s in aws_secretsmanager_secret.this : s.name]) == toset(["shortener/test/safe-browsing-api-key", "shortener/test/visitor-hash-salt"])
    error_message = "Secret names must be shortener/<env>/<name>."
  }

  assert {
    condition     = alltrue([for s in aws_secretsmanager_secret.this : s.kms_key_id == var.kms_key_arn && s.recovery_window_in_days == 0])
    error_message = "CMK encryption; immediate deletion when force_destroy (dev teardown)."
  }

  assert {
    condition     = alltrue([for d in data.aws_iam_policy_document.this : length(one(d.statement).condition) == 0 && one(d.statement).effect == "Deny"])
    error_message = "With no readers configured, GetSecretValue must be denied to everyone."
  }

  assert {
    condition     = alltrue([for p in aws_secretsmanager_secret_policy.this : p.block_public_policy])
    error_message = "Public secret policies must be blocked."
  }
}

run "readers_are_the_only_exception" {
  command = plan

  variables {
    force_destroy = false
    readers = {
      safe_browsing_api_key = ["arn:aws:iam::123456789012:role/shortener-test-api-exec"]
      visitor_hash_salt     = ["arn:aws:iam::123456789012:role/shortener-test-redirect-exec"]
    }
  }

  assert {
    condition     = toset(one(one(data.aws_iam_policy_document.this["visitor_hash_salt"].statement).condition).values) == toset(["arn:aws:iam::123456789012:role/shortener-test-redirect-exec"])
    error_message = "Only the redirect execution role may read the salt."
  }

  assert {
    condition     = alltrue([for s in aws_secretsmanager_secret.this : s.recovery_window_in_days == 30])
    error_message = "Prod keeps a 30-day recovery window."
  }
}
