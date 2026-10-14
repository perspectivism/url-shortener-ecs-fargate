mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_resource "aws_s3_bucket" {
    defaults = { arn = "arn:aws:s3:::shortener-test-bucket" }
  }
}

variables {
  name          = "shortener-test-bucket"
  force_destroy = true
}

run "kms_bucket_is_locked_down" {
  command = plan

  variables {
    kms_key_arn       = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
    access_log_bucket = "shortener-test-logs"
    expiration_days   = 7
  }

  assert {
    condition = alltrue([
      aws_s3_bucket_public_access_block.this.block_public_acls,
      aws_s3_bucket_public_access_block.this.block_public_policy,
      aws_s3_bucket_public_access_block.this.ignore_public_acls,
      aws_s3_bucket_public_access_block.this.restrict_public_buckets,
    ])
    error_message = "All four Block Public Access settings must be on."
  }

  assert {
    condition     = one(aws_s3_bucket_ownership_controls.this.rule).object_ownership == "BucketOwnerEnforced"
    error_message = "ACLs must be disabled (BucketOwnerEnforced)."
  }

  assert {
    condition     = one(aws_s3_bucket_versioning.this.versioning_configuration).status == "Enabled"
    error_message = "Versioning must be enabled."
  }

  assert {
    condition = (
      one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).sse_algorithm == "aws:kms" &&
      one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).kms_master_key_id == "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
    )
    error_message = "SSE-KMS with the environment CMK is required."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.policy.statement :
      s.effect == "Deny" && anytrue([for c in s.condition : c.variable == "aws:SecureTransport" && contains(c.values, "false")])
    ])
    error_message = "The bucket policy must deny non-TLS requests."
  }

  assert {
    condition     = length(aws_s3_bucket_logging.this) == 1 && aws_s3_bucket_logging.this[0].target_bucket == "shortener-test-logs"
    error_message = "Server access logging must be enabled when a log bucket is given."
  }

  assert {
    condition     = aws_s3_bucket.this.force_destroy
    error_message = "force_destroy must follow the variable."
  }
}

run "log_bucket_uses_sse_s3" {
  command = plan

  assert {
    condition     = one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).sse_algorithm == "AES256"
    error_message = "Without a CMK the bucket must use SSE-S3."
  }

  assert {
    condition     = length(aws_s3_bucket_logging.this) == 0
    error_message = "No access logging without a target bucket."
  }
}

run "rejects_invalid_name" {
  command = plan

  variables {
    name = "Invalid_Bucket"
  }

  expect_failures = [var.name]
}
