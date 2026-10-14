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
