terraform {
  required_version = "~> 1.16.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.68" # locked to 6.68.0 in .terraform.lock.hcl
    }
  }

  # The first apply runs with a temporary local backend (scripts/bootstrap.sh
  # writes a gitignored backend_override.tf); the state is then migrated into
  # the bucket this root creates (terraform init -migrate-state).
  backend "s3" {}
}
