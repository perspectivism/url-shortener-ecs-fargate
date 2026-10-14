mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
}

variables {
  name_prefix   = "shortener-test"
  kms_key_arn   = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  force_destroy = true
  log_bucket_id = "shortener-test-logs-123456789012"
}

run "lake_is_encrypted_projected_and_capped" {
  command = plan

  assert {
    condition     = module.events_bucket.id == "shortener-test-events-123456789012"
    error_message = "Bucket names include the account ID for global uniqueness."
  }

  assert {
    condition     = aws_athena_workgroup.this.configuration[0].enforce_workgroup_configuration && aws_athena_workgroup.this.configuration[0].bytes_scanned_cutoff_per_query == 1073741824
    error_message = "The workgroup must enforce its configuration and cap bytes scanned per query."
  }

  assert {
    condition     = aws_athena_workgroup.this.configuration[0].result_configuration[0].encryption_configuration[0].encryption_option == "SSE_KMS"
    error_message = "Athena results must be encrypted with SSE-KMS."
  }

  assert {
    condition = (
      aws_glue_catalog_table.click_events.parameters["projection.enabled"] == "true" &&
      aws_glue_catalog_table.click_events.parameters["projection.hour.range"] == "0,23" &&
      aws_glue_catalog_table.click_events.parameters["storage.location.template"] == "s3://shortener-test-events-123456789012/events/dt=$${dt}/hour=$${hour}/"
    )
    error_message = "Partition projection must match the worker's key layout events/dt=/hour=/."
  }

  assert {
    condition     = length(aws_athena_named_query.this) == 3 && alltrue([for q in aws_athena_named_query.this : strcontains(q.query, "DISTINCT event_id")])
    error_message = "Three saved queries, all deduplicating by event_id."
  }

  assert {
    condition     = aws_athena_workgroup.this.force_destroy
    error_message = "force_destroy must follow the variable."
  }
}

run "rejects_tiny_scan_cap" {
  command = plan

  variables {
    bytes_scanned_cutoff_per_query = 1024
  }

  expect_failures = [var.bytes_scanned_cutoff_per_query]
}
