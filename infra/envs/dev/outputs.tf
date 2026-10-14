output "name_prefix" {
  description = "Resource name prefix of this environment."
  value       = local.name_prefix
}

output "kms_key_arn" {
  description = "Environment CMK."
  value       = module.kms.key_arn
}

output "vpc_id" {
  description = "VPC ID."
  value       = module.network.vpc_id
}

output "links_table" {
  description = "Links table name (LINKS_TABLE)."
  value       = module.dynamodb.links_table_name
}

output "analytics_table" {
  description = "Analytics table name (ANALYTICS_TABLE)."
  value       = module.dynamodb.analytics_table_name
}

output "click_queue_url" {
  description = "Click-event queue URL (CLICK_QUEUE_URL)."
  value       = module.messaging.queue_url
}

output "click_dlq_url" {
  description = "Click-event DLQ URL."
  value       = module.messaging.dlq_url
}

output "events_bucket" {
  description = "Raw click-event bucket (EVENTS_BUCKET)."
  value       = module.events_lake.events_bucket_id
}

output "athena_workgroup" {
  description = "Athena workgroup for click analytics."
  value       = module.events_lake.athena_workgroup
}

output "cache_endpoint" {
  description = "Valkey endpoint host:port (CACHE_ADDR)."
  value       = module.cache.endpoint
}

output "cache_name" {
  description = "Valkey Serverless cache name (CACHE_NAME)."
  value       = module.cache.cache_name
}

output "cache_user" {
  description = "IAM-authenticated cache user (CACHE_USER)."
  value       = module.cache.app_user_id
}

output "secret_names" {
  description = "Secret containers to seed with make seed-secrets."
  value       = module.secrets.names
}

output "ecr_repository_urls" {
  description = "ECR repository URL per service."
  value       = module.ecr.repository_urls
}

output "services_enabled" {
  description = "Whether phase 2 (ECS services and edge) is enabled."
  value       = var.enable_services
}

output "url_scanning_enabled" {
  description = "Whether destinations are scanned (smoke tests then expect 422 for the Safe Browsing test URL)."
  value       = var.url_scanning_enabled
}

output "base_url" {
  description = "Public base URL (phase 2): smoke tests and short links."
  value       = var.enable_services ? module.edge[0].base_url : null
}

output "cloudfront_distribution_id" {
  description = "CloudFront distribution ID (phase 2)."
  value       = var.enable_services ? module.edge[0].distribution_id : null
}

output "alb_dns_name" {
  description = "Internal ALB DNS name (phase 2) — must NOT be reachable from the internet."
  value       = var.enable_services ? module.edge[0].alb_dns_name : null
}

output "ecs_cluster_name" {
  description = "ECS cluster name (phase 2)."
  value       = var.enable_services ? module.ecs_cluster[0].cluster_name : null
}

output "nightly_run_task" {
  description = "Parameters for a manual nightly run (aws ecs run-task), phase 2."
  value = var.enable_services ? {
    task_definition = module.nightly[0].task_definition_arn
    subnets         = module.network.egress_subnet_ids
    security_groups = concat([module.nightly[0].security_group_id], local.client_sgs)
  } : null
}

output "alert_topic_arn" {
  description = "SNS topic for alarms, budgets and cost anomalies (aws cloudwatch set-alarm-state test target)."
  value       = module.observability.topic_arn
}

output "alarm_names" {
  description = "CloudWatch alarm names keyed by alarm."
  value       = module.observability.alarm_names
}

output "dashboard_name" {
  description = "CloudWatch dashboard (phase 2)."
  value       = module.observability.dashboard_name
}
