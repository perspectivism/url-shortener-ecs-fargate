terraform {
  required_version = "~> 1.16.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.68" # locked to 6.68.0 in .terraform.lock.hcl
    }
  }

  # Partial configuration: values come from backend.hcl (gitignored, generated
  # by make bootstrap; see backend.hcl.example). S3 native locking, no DynamoDB.
  backend "s3" {}
}
