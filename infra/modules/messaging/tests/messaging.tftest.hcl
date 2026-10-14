mock_provider "aws" {
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_resource "aws_sqs_queue" {
    defaults = {
      arn = "arn:aws:sqs:us-east-1:123456789012:shortener-test-click-events-dlq"
      id  = "https://sqs.us-east-1.amazonaws.com/123456789012/shortener-test-click-events-dlq"
    }
  }
}

variables {
  name_prefix = "shortener-test"
  kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
}

run "queues_are_encrypted_with_redrive" {
  command = apply

  assert {
    condition     = aws_sqs_queue.clicks.kms_master_key_id == var.kms_key_arn && aws_sqs_queue.dlq.kms_master_key_id == var.kms_key_arn
    error_message = "Both queues must use SSE-KMS with the environment CMK."
  }

  assert {
    condition     = jsondecode(aws_sqs_queue.clicks.redrive_policy).maxReceiveCount == 5 && jsondecode(aws_sqs_queue.clicks.redrive_policy).deadLetterTargetArn == aws_sqs_queue.dlq.arn
    error_message = "Redrive to the DLQ after 5 receives."
  }

  assert {
    condition     = aws_sqs_queue.clicks.visibility_timeout_seconds == 120 && aws_sqs_queue.dlq.message_retention_seconds == 1209600
    error_message = "Visibility 120 s and DLQ retention 14 days (SPEC §8.2)."
  }

  assert {
    condition     = jsondecode(aws_sqs_queue_redrive_allow_policy.dlq.redrive_allow_policy).redrivePermission == "byQueue"
    error_message = "The DLQ must accept only the click queue."
  }

  assert {
    condition = alltrue([
      for d in data.aws_iam_policy_document.tls_only :
      anytrue([for s in d.statement : s.effect == "Deny" && anytrue([for c in s.condition : c.variable == "aws:SecureTransport"])])
    ])
    error_message = "Queue policies must deny non-TLS access."
  }
}

run "rejects_short_visibility" {
  command = plan

  variables {
    visibility_timeout_seconds = 30
  }

  expect_failures = [var.visibility_timeout_seconds]
}
