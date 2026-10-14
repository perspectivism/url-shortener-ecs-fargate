mock_provider "aws" {}

variables {
  name_prefix   = "shortener-test"
  kms_key_arn   = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  force_destroy = true
}

run "repositories_are_immutable_scanned_and_encrypted" {
  command = plan

  assert {
    condition     = toset(keys(aws_ecr_repository.this)) == toset(["redirect", "api", "worker", "nightly"])
    error_message = "One repository per service."
  }

  assert {
    condition = alltrue([
      for r in aws_ecr_repository.this :
      r.image_tag_mutability == "IMMUTABLE" &&
      one(r.image_scanning_configuration).scan_on_push &&
      one(r.encryption_configuration).encryption_type == "KMS" &&
      one(r.encryption_configuration).kms_key == var.kms_key_arn &&
      r.force_delete
    ])
    error_message = "Immutable tags, scan on push, CMK encryption, force_delete following the variable."
  }

  assert {
    condition     = alltrue([for p in aws_ecr_lifecycle_policy.this : length(jsondecode(p.policy).rules) == 2])
    error_message = "Every repository needs the lifecycle policy."
  }
}

run "rejects_tiny_history" {
  command = plan

  variables {
    keep_images = 1
  }

  expect_failures = [var.keep_images]
}
