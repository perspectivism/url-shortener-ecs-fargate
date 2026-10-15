output "state_bucket" {
  description = "Terraform state bucket (backend.hcl bucket)."
  value       = module.state_bucket.id
}

output "state_kms_key_arn" {
  description = "CMK encrypting Terraform state (backend.hcl kms_key_id)."
  value       = aws_kms_key.state.arn
}

output "plan_role_arn" {
  description = "Role for pull-request plans (GitHub variable AWS_PLAN_ROLE_ARN)."
  value       = aws_iam_role.plan.arn
}

output "deploy_plan_role_arn" {
  description = "Role for the deploy workflow's plan jobs on main (repository variable AWS_DEPLOY_PLAN_ROLE_ARN)."
  value       = aws_iam_role.deploy_plan.arn
}

output "apply_role_arns" {
  description = "Apply role per GitHub environment (environment variable AWS_APPLY_ROLE_ARN)."
  value       = { for k, r in aws_iam_role.apply : k => r.arn }
}

output "permissions_boundary_arn" {
  description = "Boundary attached to every CI role and every role CI creates."
  value       = aws_iam_policy.boundary.arn
}
