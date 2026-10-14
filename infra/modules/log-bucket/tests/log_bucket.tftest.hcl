mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "us-east-1" }
  }
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
}

variables {
  name           = "shortener-test-logs"
  force_destroy  = true
  retention_days = 14
}

run "log_delivery_is_scoped_to_this_account" {
  command = plan

  assert {
    condition     = length(data.aws_iam_policy_document.delivery.statement) == 3
    error_message = "Exactly three log-delivery grants are expected."
  }

  assert {
    condition = alltrue([
      for s in data.aws_iam_policy_document.delivery.statement :
      length(s.condition) > 0 && s.actions == toset(["s3:PutObject"])
    ])
    error_message = "Every delivery grant must be PutObject-only and conditioned on this account."
  }

  assert {
    condition     = contains(flatten([for s in data.aws_iam_policy_document.delivery.statement : s.resources]), "arn:aws:s3:::shortener-test-logs/alb/AWSLogs/123456789012/*")
    error_message = "ALB delivery must be limited to this account's prefix."
  }
}

run "rejects_bad_retention" {
  command = plan

  variables {
    retention_days = 0
  }

  expect_failures = [var.retention_days]
}
