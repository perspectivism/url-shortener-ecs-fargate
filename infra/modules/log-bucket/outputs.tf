output "id" {
  description = "Log bucket name."
  value       = module.bucket.id
}

output "arn" {
  description = "Log bucket ARN."
  value       = module.bucket.arn
}
