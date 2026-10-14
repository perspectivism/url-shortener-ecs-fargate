output "alb_arn" {
  description = "Internal ALB ARN."
  value       = aws_lb.this.arn
}

output "alb_arn_suffix" {
  description = "ALB ARN suffix (CloudWatch dimension)."
  value       = aws_lb.this.arn_suffix
}

output "alb_dns_name" {
  description = "Internal ALB DNS name (resolves to private IPs; not reachable from the internet)."
  value       = aws_lb.this.dns_name
}

output "alb_security_group_id" {
  description = "ALB security group ID."
  value       = aws_security_group.alb.id
}

output "target_group_arns" {
  description = "Target group ARN per service."
  value       = { for k, tg in aws_lb_target_group.this : k => tg.arn }
}

output "target_group_arn_suffixes" {
  description = "Target group ARN suffix per service (CloudWatch dimension)."
  value       = { for k, tg in aws_lb_target_group.this : k => tg.arn_suffix }
}

output "distribution_id" {
  description = "CloudFront distribution ID."
  value       = aws_cloudfront_distribution.this.id
}

output "distribution_domain_name" {
  description = "CloudFront domain name."
  value       = aws_cloudfront_distribution.this.domain_name
}

output "base_url" {
  description = "Public base URL (BASE_URL)."
  value       = "https://${coalesce(var.domain_name, aws_cloudfront_distribution.this.domain_name)}"
}

output "web_acl_arn" {
  description = "WAF web ACL ARN."
  value       = aws_wafv2_web_acl.this.arn
}

output "web_acl_name" {
  description = "WAF web ACL name (CloudWatch dimension)."
  value       = aws_wafv2_web_acl.this.name
}

output "routing_ready" {
  description = "Per service, the listener (redirect, default action) or listener rule (api) that attaches its target group; services depend on it."
  value = {
    redirect = local.listener_tls ? aws_lb_listener.https[0].arn : aws_lb_listener.http[0].arn
    api      = aws_lb_listener_rule.api.arn
  }
}
