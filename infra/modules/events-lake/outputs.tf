output "events_bucket_id" {
  description = "Raw click-event bucket name."
  value       = module.events_bucket.id
}

output "events_bucket_arn" {
  description = "Raw click-event bucket ARN."
  value       = module.events_bucket.arn
}

output "athena_results_bucket_id" {
  description = "Athena query results bucket name."
  value       = module.athena_results.id
}

output "athena_workgroup" {
  description = "Athena workgroup name."
  value       = aws_athena_workgroup.this.name
}

output "glue_database" {
  description = "Glue database holding the click_events table."
  value       = aws_glue_catalog_database.this.name
}
