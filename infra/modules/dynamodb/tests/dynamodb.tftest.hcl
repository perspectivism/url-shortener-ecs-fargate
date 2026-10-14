mock_provider "aws" {}

variables {
  name_prefix         = "shortener-test"
  kms_key_arn         = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  deletion_protection = false
}

run "tables_are_encrypted_recoverable_and_capped" {
  command = plan

  assert {
    condition = alltrue([
      for t in [aws_dynamodb_table.links, aws_dynamodb_table.analytics] :
      one(t.point_in_time_recovery).enabled &&
      one(t.server_side_encryption).enabled &&
      one(t.server_side_encryption).kms_key_arn == var.kms_key_arn &&
      one(t.ttl).attribute_name == "ttl" &&
      t.billing_mode == "PAY_PER_REQUEST" &&
      one(t.on_demand_throughput).max_write_request_units >= 1
    ])
    error_message = "Both tables need PITR, CMK encryption, TTL on ttl, on-demand billing and a throughput cap."
  }

  assert {
    condition     = aws_dynamodb_table.links.name == "shortener-test-links" && aws_dynamodb_table.analytics.name == "shortener-test-analytics"
    error_message = "Table names must follow the naming convention."
  }

  assert {
    condition = (
      one(aws_dynamodb_table.links.global_secondary_index).name == "gsi1" &&
      one(aws_dynamodb_table.links.global_secondary_index).projection_type == "INCLUDE" &&
      toset(one(aws_dynamodb_table.links.global_secondary_index).non_key_attributes) == toset(["long_url", "status", "created_at", "expires_at"])
    )
    error_message = "gsi1 must match internal/store/schema.go."
  }

  assert {
    condition     = !aws_dynamodb_table.links.deletion_protection_enabled && !aws_dynamodb_table.analytics.deletion_protection_enabled
    error_message = "Deletion protection must follow the variable."
  }
}

run "prod_enables_deletion_protection" {
  command = plan

  variables {
    deletion_protection = true
  }

  assert {
    condition     = aws_dynamodb_table.links.deletion_protection_enabled && aws_dynamodb_table.analytics.deletion_protection_enabled
    error_message = "Prod tables must have deletion protection."
  }
}

run "rejects_zero_caps" {
  command = plan

  variables {
    links_max_write_units = 0
  }

  expect_failures = [var.links_max_write_units]
}
