# Environment root. Identical in envs/dev and envs/prod (enforced by
# scripts/check-env-parity.sh); only variable defaults differ.
#
# Two-phase deploy without -target (SPEC §12.1): enable_services = false
# builds the foundation (network, data, secrets, ECR); services are added in
# M5 once secrets are seeded and images pushed.

provider "aws" {
  region = var.region

  default_tags {
    tags = {
      Project     = var.project
      Environment = var.environment
      ManagedBy   = "terraform"
      Repository  = var.repository
      Owner       = var.owner
      # Budgets and Cost Anomaly Detection filter on this one tag (ADR-0022).
      CostCenter = "${var.project}-${var.environment}"
    }
  }
}

data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

locals {
  name_prefix = "${var.project}-${var.environment}"
  account_id  = data.aws_caller_identity.current.account_id
  # Created by infra/bootstrap: every role in this project carries it, so CI
  # (which may only create roles with this boundary) can manage them all.
  permissions_boundary_arn = "arn:${data.aws_partition.current.partition}:iam::${local.account_id}:policy/${var.project}-ci-boundary"
}

module "kms" {
  source = "../../modules/kms"

  name_prefix = local.name_prefix
  log_group_name_prefixes = [
    "/${var.project}/${var.environment}/",
    "aws-waf-logs-${local.name_prefix}",
    "/aws/ecs/containerinsights/${local.name_prefix}/",
  ]
}

module "logs" {
  source = "../../modules/log-bucket"

  name           = "${local.name_prefix}-logs-${local.account_id}"
  force_destroy  = var.force_destroy
  retention_days = var.log_retention_days
}

module "network" {
  source = "../../modules/network"

  name_prefix                 = local.name_prefix
  az_count                    = var.az_count
  nat_enabled                 = var.url_scanning_enabled
  interface_endpoint_az_count = var.interface_endpoint_az_count
  ecs_exec_enabled            = var.ecs_exec_enabled
  kms_key_arn                 = module.kms.key_arn
  flow_log_retention_days     = var.log_retention_days
  dynamodb_table_name_prefix  = "${local.name_prefix}-"
  s3_bucket_name_prefix       = "${local.name_prefix}-"
  permissions_boundary_arn    = local.permissions_boundary_arn
}

module "dynamodb" {
  source = "../../modules/dynamodb"

  name_prefix         = local.name_prefix
  kms_key_arn         = module.kms.key_arn
  deletion_protection = !var.force_destroy
}

module "messaging" {
  source = "../../modules/messaging"

  name_prefix = local.name_prefix
  kms_key_arn = module.kms.key_arn
}

module "events_lake" {
  source = "../../modules/events-lake"

  name_prefix           = local.name_prefix
  kms_key_arn           = module.kms.key_arn
  force_destroy         = var.force_destroy
  log_bucket_id         = module.logs.id
  events_retention_days = var.events_retention_days
}

module "cache" {
  source = "../../modules/cache"

  name_prefix         = local.name_prefix
  kms_key_arn         = module.kms.key_arn
  vpc_id              = module.network.vpc_id
  subnet_ids          = module.network.isolated_subnet_ids
  max_data_storage_gb = var.cache_max_data_storage_gb
  max_ecpu_per_second = var.cache_max_ecpu_per_second
}

module "secrets" {
  source = "../../modules/secrets"

  name_path     = "${var.project}/${var.environment}"
  kms_key_arn   = module.kms.key_arn
  force_destroy = var.force_destroy

  # Only the consuming services' execution roles may read values (ADR-0014).
  readers = {
    visitor_hash_salt     = var.enable_services ? [local.exec_role_arn["redirect"]] : []
    safe_browsing_api_key = var.enable_services && var.url_scanning_enabled ? [local.exec_role_arn["api"], local.exec_role_arn["nightly"]] : []
  }
}

module "ecr" {
  source = "../../modules/ecr"

  name_prefix   = local.name_prefix
  kms_key_arn   = module.kms.key_arn
  force_destroy = var.force_destroy
}
