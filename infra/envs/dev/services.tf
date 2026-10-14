# Phase 2 (enable_services = true): ECS services, the nightly job and the edge.
# Prerequisites: secrets seeded (make seed-secrets) and images pushed with
# image_tag (make push-images). Identical in envs/dev and envs/prod.

locals {
  services_on = var.enable_services
  svc         = local.services_on ? 1 : 0
  region      = var.region
  partition   = data.aws_partition.current.partition

  image = { for s in ["redirect", "api", "worker", "nightly"] : s => "${module.ecr.repository_urls[s]}:${coalesce(var.image_tag, "unset")}" }

  # Execution roles have deterministic names, so secret policies can name them
  # without depending on the service modules.
  exec_role_arn = { for s in ["redirect", "api", "nightly"] : s => "arn:${local.partition}:iam::${local.account_id}:role/${local.name_prefix}-${s}-exec" }

  scanner = var.url_scanning_enabled ? "safebrowsing" : "noop"
  scanner_secrets = var.url_scanning_enabled ? {
    SAFE_BROWSING_API_KEY = module.secrets.safe_browsing_api_key_arn
  } : {}

  common_env = {
    APP_ENV    = var.environment
    AWS_REGION = var.region
    LOG_LEVEL  = "info"
    HTTP_ADDR  = ":8080"
    CACHE_ADDR = module.cache.endpoint
    CACHE_MODE = "cluster"
    CACHE_AUTH = "iam"
    CACHE_TLS  = "true"
    CACHE_USER = module.cache.app_user_id
    CACHE_NAME = module.cache.cache_name
  }

  gateway_egress = {
    s3_prefix_list_id       = module.network.s3_prefix_list_id
    dynamodb_prefix_list_id = module.network.dynamodb_prefix_list_id
  }

  client_sgs = [module.network.endpoint_client_security_group_id, module.cache.client_security_group_id]
}

# --- Least-privilege task role policies -------------------------------------------------

data "aws_iam_policy_document" "cache_connect" {
  statement {
    sid       = "ValkeyIAMAuth"
    effect    = "Allow"
    actions   = ["elasticache:Connect"]
    resources = [module.cache.cache_arn, module.cache.app_user_arn]
  }
}

data "aws_iam_policy_document" "redirect" {
  source_policy_documents = [data.aws_iam_policy_document.cache_connect.json]

  statement {
    sid       = "ResolveLinks"
    effect    = "Allow"
    actions   = ["dynamodb:GetItem"]
    resources = [module.dynamodb.links_table_arn]
  }

  statement {
    sid       = "EnqueueClicks"
    effect    = "Allow"
    actions   = ["sqs:SendMessage"]
    resources = [module.messaging.queue_arn]
  }

  statement {
    sid       = "CMKViaDynamoDBAndSQS"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [module.kms.key_arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["dynamodb.${local.region}.amazonaws.com", "sqs.${local.region}.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "api" {
  source_policy_documents = [data.aws_iam_policy_document.cache_connect.json]

  statement {
    sid       = "ManageLinksAndKeys"
    effect    = "Allow"
    actions   = ["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:Query"]
    resources = [module.dynamodb.links_table_arn, "${module.dynamodb.links_table_arn}/index/gsi1"]
  }

  statement {
    sid       = "ReadStats"
    effect    = "Allow"
    actions   = ["dynamodb:Query"]
    resources = [module.dynamodb.analytics_table_arn]
  }

  statement {
    sid       = "CMKViaDynamoDB"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [module.kms.key_arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["dynamodb.${local.region}.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "worker" {
  source_policy_documents = [data.aws_iam_policy_document.cache_connect.json]

  statement {
    sid       = "ConsumeClicks"
    effect    = "Allow"
    actions   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:ChangeMessageVisibility", "sqs:GetQueueAttributes"]
    resources = [module.messaging.queue_arn]
  }

  statement {
    sid       = "CountClicks"
    effect    = "Allow"
    actions   = ["dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:ConditionCheckItem"]
    resources = [module.dynamodb.analytics_table_arn]
  }

  statement {
    sid       = "WriteRawEvents"
    effect    = "Allow"
    actions   = ["s3:PutObject"]
    resources = ["${module.events_lake.events_bucket_arn}/events/*"]
  }

  statement {
    sid       = "CMKViaServices"
    effect    = "Allow"
    actions   = ["kms:Decrypt", "kms:GenerateDataKey"]
    resources = [module.kms.key_arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["dynamodb.${local.region}.amazonaws.com", "sqs.${local.region}.amazonaws.com", "s3.${local.region}.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "nightly" {
  source_policy_documents = [data.aws_iam_policy_document.cache_connect.json]

  statement {
    sid       = "RescanAndDisableLinks"
    effect    = "Allow"
    actions   = ["dynamodb:Scan", "dynamodb:GetItem", "dynamodb:PutItem"]
    resources = [module.dynamodb.links_table_arn]
  }

  statement {
    sid       = "PersistUniques"
    effect    = "Allow"
    actions   = ["dynamodb:UpdateItem"]
    resources = [module.dynamodb.analytics_table_arn]
  }

  statement {
    sid       = "CMKViaDynamoDB"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [module.kms.key_arn]

    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["dynamodb.${local.region}.amazonaws.com"]
    }
  }
}

# --- Cluster, edge, services -------------------------------------------------------------

module "ecs_cluster" {
  source = "../../modules/ecs-cluster"
  count  = local.svc

  name_prefix        = local.name_prefix
  kms_key_arn        = module.kms.key_arn
  log_retention_days = var.log_retention_days
}

module "edge" {
  source = "../../modules/edge"
  count  = local.svc

  name_prefix                    = local.name_prefix
  vpc_id                         = module.network.vpc_id
  subnet_ids                     = module.network.isolated_subnet_ids
  target_security_group_ids      = { redirect = module.redirect[0].security_group_id, api = module.api[0].security_group_id }
  log_bucket_id                  = module.logs.id
  us_east_1_kms_key_arn          = module.kms.key_arn
  log_retention_days             = var.log_retention_days
  force_destroy                  = var.force_destroy
  domain_name                    = var.domain_name
  hosted_zone_id                 = var.hosted_zone_id
  api_rate_limit_per_minute      = var.waf_api_rate_limit_per_minute
  redirect_rate_limit_per_minute = var.waf_redirect_rate_limit_per_minute
  anonymous_ip_action            = var.waf_anonymous_ip_action
}

module "redirect" {
  source = "../../modules/ecs-service"
  count  = local.svc

  name_prefix              = local.name_prefix
  service                  = "redirect"
  cluster_arn              = module.ecs_cluster[0].cluster_arn
  cluster_name             = module.ecs_cluster[0].cluster_name
  image                    = local.image["redirect"]
  ecr_repository_arn       = module.ecr.repository_arns["redirect"]
  vpc_id                   = module.network.vpc_id
  subnet_ids               = module.network.isolated_subnet_ids
  extra_security_group_ids = local.client_sgs
  egress                   = merge(local.gateway_egress, { internet_https = false })
  kms_key_arn              = module.kms.key_arn
  log_retention_days       = var.log_retention_days
  permissions_boundary_arn = local.permissions_boundary_arn
  enable_execute_command   = var.ecs_exec_enabled
  task_policy_json         = data.aws_iam_policy_document.redirect.json
  desired_count            = var.redirect_min_count
  min_count                = var.redirect_min_count
  max_count                = var.redirect_max_count
  alb_requests_per_target  = 500
  cpu_target_percent       = 60
  capacity_provider_strategy = var.redirect_on_spot ? [
    { capacity_provider = "FARGATE_SPOT", base = 0, weight = 1 },
    ] : [
    { capacity_provider = "FARGATE", base = var.redirect_min_count, weight = 1 },
  ]

  load_balancer = {
    target_group_arn      = module.edge[0].target_group_arns["redirect"]
    alb_security_group_id = module.edge[0].alb_security_group_id
    alb_resource_label    = "${module.edge[0].alb_arn_suffix}/${module.edge[0].target_group_arn_suffixes["redirect"]}"
    routing_ready         = module.edge[0].routing_ready["redirect"]
  }

  environment = merge(local.common_env, {
    LINKS_TABLE      = module.dynamodb.links_table_name
    CLICK_QUEUE_URL  = module.messaging.queue_url
    CLIENT_IP_SOURCE = "cloudfront"
  })
  secrets = { VISITOR_HASH_SALT = module.secrets.visitor_hash_salt_arn }
}

module "api" {
  source = "../../modules/ecs-service"
  count  = local.svc

  name_prefix              = local.name_prefix
  service                  = "api"
  cluster_arn              = module.ecs_cluster[0].cluster_arn
  cluster_name             = module.ecs_cluster[0].cluster_name
  image                    = local.image["api"]
  ecr_repository_arn       = module.ecr.repository_arns["api"]
  vpc_id                   = module.network.vpc_id
  subnet_ids               = module.network.egress_subnet_ids
  extra_security_group_ids = local.client_sgs
  egress                   = merge(local.gateway_egress, { internet_https = var.url_scanning_enabled })
  kms_key_arn              = module.kms.key_arn
  log_retention_days       = var.log_retention_days
  permissions_boundary_arn = local.permissions_boundary_arn
  enable_execute_command   = var.ecs_exec_enabled
  task_policy_json         = data.aws_iam_policy_document.api.json
  desired_count            = var.api_min_count
  min_count                = var.api_min_count
  max_count                = var.api_max_count
  cpu_target_percent       = 60
  capacity_provider_strategy = [
    { capacity_provider = "FARGATE", base = var.api_min_count, weight = 1 },
  ]

  load_balancer = {
    target_group_arn      = module.edge[0].target_group_arns["api"]
    alb_security_group_id = module.edge[0].alb_security_group_id
    alb_resource_label    = "${module.edge[0].alb_arn_suffix}/${module.edge[0].target_group_arn_suffixes["api"]}"
    routing_ready         = module.edge[0].routing_ready["api"]
  }

  environment = merge(local.common_env, {
    LINKS_TABLE     = module.dynamodb.links_table_name
    ANALYTICS_TABLE = module.dynamodb.analytics_table_name
    BASE_URL        = module.edge[0].base_url
    BLOCKED_HOSTS   = module.edge[0].distribution_domain_name
    SQIDS_ALPHABET  = var.sqids_alphabet
    SCANNER         = local.scanner
  })
  secrets = local.scanner_secrets
}

module "worker" {
  source = "../../modules/ecs-service"
  count  = local.svc

  name_prefix              = local.name_prefix
  service                  = "worker"
  cluster_arn              = module.ecs_cluster[0].cluster_arn
  cluster_name             = module.ecs_cluster[0].cluster_name
  image                    = local.image["worker"]
  ecr_repository_arn       = module.ecr.repository_arns["worker"]
  vpc_id                   = module.network.vpc_id
  subnet_ids               = module.network.isolated_subnet_ids
  extra_security_group_ids = local.client_sgs
  egress                   = merge(local.gateway_egress, { internet_https = false })
  kms_key_arn              = module.kms.key_arn
  log_retention_days       = var.log_retention_days
  permissions_boundary_arn = local.permissions_boundary_arn
  enable_execute_command   = var.ecs_exec_enabled
  task_policy_json         = data.aws_iam_policy_document.worker.json
  desired_count            = var.worker_min_count
  min_count                = var.worker_min_count
  max_count                = var.worker_max_count
  cpu_target_percent       = null
  sqs_queue_name           = module.messaging.queue_name
  wait_for_steady_state    = var.worker_min_count > 0
  capacity_provider_strategy = [
    { capacity_provider = "FARGATE_SPOT", base = 0, weight = 4 },
    { capacity_provider = "FARGATE", base = 0, weight = 1 },
  ]

  environment = merge(local.common_env, {
    ANALYTICS_TABLE = module.dynamodb.analytics_table_name
    CLICK_QUEUE_URL = module.messaging.queue_url
    EVENTS_BUCKET   = module.events_lake.events_bucket_id
  })
}

module "nightly" {
  source = "../../modules/scheduled-task"
  count  = local.svc

  name_prefix              = local.name_prefix
  task                     = "nightly"
  cluster_arn              = module.ecs_cluster[0].cluster_arn
  image                    = local.image["nightly"]
  ecr_repository_arn       = module.ecr.repository_arns["nightly"]
  vpc_id                   = module.network.vpc_id
  subnet_ids               = module.network.egress_subnet_ids
  extra_security_group_ids = local.client_sgs
  egress                   = merge(local.gateway_egress, { internet_https = var.url_scanning_enabled })
  kms_key_arn              = module.kms.key_arn
  log_retention_days       = var.log_retention_days
  permissions_boundary_arn = local.permissions_boundary_arn
  task_policy_json         = data.aws_iam_policy_document.nightly.json

  environment = merge(local.common_env, {
    LINKS_TABLE     = module.dynamodb.links_table_name
    ANALYTICS_TABLE = module.dynamodb.analytics_table_name
    SCANNER         = local.scanner
  })
  secrets = local.scanner_secrets
}
