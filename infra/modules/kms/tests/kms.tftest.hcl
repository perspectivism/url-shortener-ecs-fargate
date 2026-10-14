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
  name_prefix             = "shortener-test"
  log_group_name_prefixes = ["/shortener/test/", "aws-waf-logs-shortener-test"]
}

run "cmk_rotates_and_is_scoped" {
  command = plan

  assert {
    condition     = aws_kms_key.this.enable_key_rotation
    error_message = "Key rotation must be enabled."
  }

  assert {
    condition     = aws_kms_key.this.deletion_window_in_days == 7
    error_message = "Deletion window must default to the 7-day minimum (fast, complete teardown)."
  }

  assert {
    condition     = aws_kms_alias.this.name == "alias/shortener-test"
    error_message = "Alias must follow the naming convention."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.key.statement :
      s.sid == "AllowCloudWatchLogs" && anytrue([for c in s.condition : contains(c.values, "arn:aws:logs:us-east-1:123456789012:log-group:/shortener/test/*") && contains(c.values, "arn:aws:logs:us-east-1:123456789012:log-group:aws-waf-logs-shortener-test*")])
    ])
    error_message = "CloudWatch Logs access must be limited to this environment's log groups."
  }
}

run "alert_publishers_are_account_scoped" {
  command = plan

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.key.statement :
      s.sid == "AllowAlertPublishersToEncryptedSNS" &&
      anytrue([for c in s.condition : c.variable == "aws:SourceAccount" && contains(c.values, "123456789012")])
    ])
    error_message = "Service principals may use the key only on behalf of this account."
  }
}

run "rejects_bad_inputs" {
  command = plan

  variables {
    name_prefix             = "Bad_Name"
    deletion_window_in_days = 3
  }

  expect_failures = [var.name_prefix, var.deletion_window_in_days]
}
