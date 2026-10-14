# Edge (ADR-0012): CloudFront + WAF is the only ingress. The ALB is internal,
# in isolated subnets, reachable only through a CloudFront VPC origin; its
# security group admits only CloudFront's origin-facing prefix list.
# Redirects are never cached at the edge (CachingDisabled on both behaviors).

data "aws_ec2_managed_prefix_list" "cloudfront_origin" {
  name = "com.amazonaws.global.cloudfront.origin-facing"
}

data "aws_cloudfront_cache_policy" "caching_disabled" {
  name = "Managed-CachingDisabled"
}

data "aws_cloudfront_response_headers_policy" "security_headers" {
  name = "Managed-SecurityHeadersPolicy"
}

locals {
  # CloudFront-scope WAF, the viewer certificate and CloudFront/WAF log groups
  # must live in us-east-1 (AWS provider 6.x per-resource region argument).
  global_region = "us-east-1"
  custom        = var.domain_name != null
  origin_host   = local.custom ? "origin.${var.domain_name}" : aws_lb.this.dns_name
  listener_tls  = local.custom
  origin_id     = "alb"
}

# --- Internal ALB --------------------------------------------------------------------

resource "aws_security_group" "alb" {
  name        = "${var.name_prefix}-alb"
  description = "Internal ALB: CloudFront origin-facing addresses only"
  vpc_id      = var.vpc_id
  tags        = { Name = "${var.name_prefix}-alb" }
}

resource "aws_vpc_security_group_ingress_rule" "alb_from_cloudfront" {
  security_group_id = aws_security_group.alb.id
  description       = "CloudFront VPC origin (origin-facing managed prefix list)"
  ip_protocol       = "tcp"
  from_port         = local.listener_tls ? 443 : 80
  to_port           = local.listener_tls ? 443 : 80
  prefix_list_id    = data.aws_ec2_managed_prefix_list.cloudfront_origin.id
}

resource "aws_vpc_security_group_egress_rule" "alb_to_targets" {
  for_each = var.target_security_group_ids

  security_group_id            = aws_security_group.alb.id
  description                  = "To ${each.key} tasks"
  ip_protocol                  = "tcp"
  from_port                    = var.container_port
  to_port                      = var.container_port
  referenced_security_group_id = each.value
}

resource "aws_lb" "this" {
  #checkov:skip=CKV_AWS_150:Deletion protection is enabled whenever force_destroy is false (prod); dev allows one-command teardown.
  #checkov:skip=CKV2_AWS_20:Viewers are redirected to HTTPS by CloudFront; the internal ALB is never reachable by viewers.
  #checkov:skip=CKV2_AWS_28:Internal ALB behind CloudFront; the WAF web ACL is attached to the CloudFront distribution (the only ingress).
  name                       = var.name_prefix
  internal                   = true
  load_balancer_type         = "application"
  subnets                    = var.subnet_ids
  security_groups            = [aws_security_group.alb.id]
  drop_invalid_header_fields = true
  enable_deletion_protection = !var.force_destroy
  idle_timeout               = 60

  access_logs {
    enabled = true
    bucket  = var.log_bucket_id
    prefix  = "alb"
  }
}

resource "aws_lb_target_group" "this" {
  #checkov:skip=CKV_AWS_378:ALB-to-task traffic is HTTP inside isolated subnets (SG-to-SG only); TLS to tasks would need per-task certificates (accepted risk, threat model).
  for_each = toset(["redirect", "api"])

  name                 = "${var.name_prefix}-${each.key}"
  target_type          = "ip"
  port                 = var.container_port
  protocol             = "HTTP"
  vpc_id               = var.vpc_id
  deregistration_delay = 15

  health_check {
    path                = "/healthz"
    matcher             = "200"
    interval            = 10
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }
}

# Trivy AWS-0054 suppressed: Internal VPC-origin hop is HTTP only without a custom domain (accepted risk, SPEC §11.3); domain_name switches to the HTTPS listener.
#trivy:ignore:AWS-0054
resource "aws_lb_listener" "http" {
  #checkov:skip=CKV_AWS_2:Without a custom domain the CloudFront->ALB hop is HTTP inside the AWS network via the VPC origin (accepted risk, threat model); with domain_name the HTTPS listener is used instead.
  #checkov:skip=CKV_AWS_103:Same as CKV_AWS_2 (no TLS on the internal hop without a custom domain).
  #checkov:skip=CKV2_AWS_20:Viewers are redirected to HTTPS by CloudFront; this internal listener is never reachable by viewers.
  count = local.listener_tls ? 0 : 1

  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this["redirect"].arn
  }
}

resource "aws_lb_listener" "https" {
  count = local.listener_tls ? 1 : 0

  load_balancer_arn = aws_lb.this.arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = aws_acm_certificate_validation.origin[0].certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this["redirect"].arn
  }
}

resource "aws_lb_listener_rule" "api" {
  listener_arn = local.listener_tls ? aws_lb_listener.https[0].arn : aws_lb_listener.http[0].arn
  priority     = 10

  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this["api"].arn
  }

  condition {
    path_pattern {
      values = ["/api/*"]
    }
  }
}

# --- CloudFront -----------------------------------------------------------------------

resource "aws_cloudfront_vpc_origin" "alb" {
  vpc_origin_endpoint_config {
    name                   = var.name_prefix
    arn                    = aws_lb.this.arn
    http_port              = 80
    https_port             = 443
    origin_protocol_policy = local.listener_tls ? "https-only" : "http-only"

    origin_ssl_protocols {
      items    = ["TLSv1.2"]
      quantity = 1
    }
  }
}

# Everything the services need from the viewer request (SPEC §12.5). Nothing is
# cached, so forwarding Authorization cannot leak responses between callers.
resource "aws_cloudfront_origin_request_policy" "this" {
  name    = "${var.name_prefix}-origin"
  comment = "Forward auth, content, client hints and viewer location; no cookies"

  cookies_config {
    cookie_behavior = "none"
  }

  headers_config {
    header_behavior = "whitelist"
    headers {
      items = [
        "Authorization", "Content-Type", "Idempotency-Key", "User-Agent", "Referer",
        "CloudFront-Viewer-Country", "CloudFront-Viewer-Address",
      ]
    }
  }

  query_strings_config {
    query_string_behavior = "all"
  }
}

resource "aws_cloudfront_distribution" "this" {
  #checkov:skip=CKV_AWS_305:"/" is handled by the redirect service (404); there is no static root object.
  #checkov:skip=CKV_AWS_86:Access logs use CloudFront standard logging v2 (CloudWatch Logs delivery below), not the legacy logging_config.
  #checkov:skip=CKV_AWS_174:With the default *.cloudfront.net certificate AWS fixes the viewer security policy; domain_name enables TLSv1.2_2021 (accepted, threat model).
  #checkov:skip=CKV2_AWS_42:A custom certificate requires the optional custom domain (domain_name).
  #checkov:skip=CKV_AWS_310:Single origin by design; origin failover would need a second region (out of scope).
  #checkov:skip=CKV_AWS_374:A global URL shortener does not geo-restrict; WAF IP reputation and rate rules apply instead.
  enabled         = true
  is_ipv6_enabled = true
  http_version    = "http2and3"
  price_class     = "PriceClass_100"
  comment         = var.name_prefix
  aliases         = local.custom ? [var.domain_name] : []
  web_acl_id      = aws_wafv2_web_acl.this.arn

  origin {
    origin_id   = local.origin_id
    domain_name = local.origin_host

    vpc_origin_config {
      vpc_origin_id            = aws_cloudfront_vpc_origin.alb.id
      origin_keepalive_timeout = 5
      origin_read_timeout      = 30
    }
  }

  # Redirects: GET/HEAD /{code}, never cached (ADR-0007).
  default_cache_behavior {
    target_origin_id           = local.origin_id
    viewer_protocol_policy     = "redirect-to-https"
    allowed_methods            = ["GET", "HEAD"]
    cached_methods             = ["GET", "HEAD"]
    cache_policy_id            = data.aws_cloudfront_cache_policy.caching_disabled.id
    origin_request_policy_id   = aws_cloudfront_origin_request_policy.this.id
    response_headers_policy_id = data.aws_cloudfront_response_headers_policy.security_headers.id
    compress                   = false
  }

  ordered_cache_behavior {
    path_pattern               = "/api/*"
    target_origin_id           = local.origin_id
    viewer_protocol_policy     = "https-only"
    allowed_methods            = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods             = ["GET", "HEAD"]
    cache_policy_id            = data.aws_cloudfront_cache_policy.caching_disabled.id
    origin_request_policy_id   = aws_cloudfront_origin_request_policy.this.id
    response_headers_policy_id = data.aws_cloudfront_response_headers_policy.security_headers.id
    compress                   = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = !local.custom
    acm_certificate_arn            = local.custom ? aws_acm_certificate_validation.viewer[0].certificate_arn : null
    ssl_support_method             = local.custom ? "sni-only" : null
    minimum_protocol_version       = local.custom ? "TLSv1.2_2021" : "TLSv1"
  }
}

# CloudFront standard logging v2 -> CloudWatch Logs (us-east-1), with retention.
resource "aws_cloudwatch_log_group" "cloudfront" {
  #checkov:skip=CKV_AWS_338:Retention is 14 days (dev) / 90 days (prod) per SPEC §12.6.
  region = local.global_region

  name              = "/${replace(var.name_prefix, "-", "/")}/cloudfront"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.us_east_1_kms_key_arn
}

resource "aws_cloudwatch_log_delivery_source" "cloudfront" {
  region = local.global_region

  name         = "${var.name_prefix}-cloudfront"
  log_type     = "ACCESS_LOGS"
  resource_arn = aws_cloudfront_distribution.this.arn
}

resource "aws_cloudwatch_log_delivery_destination" "cloudfront" {
  region = local.global_region

  name          = "${var.name_prefix}-cloudfront"
  output_format = "json"

  delivery_destination_configuration {
    destination_resource_arn = aws_cloudwatch_log_group.cloudfront.arn
  }
}

resource "aws_cloudwatch_log_delivery" "cloudfront" {
  region = local.global_region

  delivery_source_name     = aws_cloudwatch_log_delivery_source.cloudfront.name
  delivery_destination_arn = aws_cloudwatch_log_delivery_destination.cloudfront.arn
}

# --- WAF (CLOUDFRONT scope, us-east-1) ----------------------------------------------------

resource "aws_wafv2_web_acl" "this" {
  region = local.global_region

  name        = var.name_prefix
  description = "URL Shortener edge protection (${var.name_prefix})"
  scope       = "CLOUDFRONT"

  default_action {
    allow {}
  }

  custom_response_body {
    key          = "rate_limited"
    content_type = "APPLICATION_JSON"
    content      = jsonencode({ type = "urn:shortener:problem:rate_limited", title = "Too Many Requests", status = 429, code = "rate_limited", detail = "request rate limit exceeded" })
  }

  rule {
    name     = "AWSManagedRulesAmazonIpReputationList"
    priority = 10

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        vendor_name = "AWS"
        name        = "AWSManagedRulesAmazonIpReputationList"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-AWSManagedRulesAmazonIpReputationList"
      sampled_requests_enabled   = true
    }
  }

  # Initially count-only (SPEC §11.3); switch to block after reviewing WAF logs.
  rule {
    name     = "AWSManagedRulesAnonymousIpList"
    priority = 20

    override_action {
      dynamic "count" {
        for_each = var.anonymous_ip_action == "count" ? [1] : []
        content {}
      }
      dynamic "none" {
        for_each = var.anonymous_ip_action == "count" ? [] : [1]
        content {}
      }
    }

    statement {
      managed_rule_group_statement {
        vendor_name = "AWS"
        name        = "AWSManagedRulesAnonymousIpList"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-AWSManagedRulesAnonymousIpList"
      sampled_requests_enabled   = true
    }
  }

  # Includes the Log4j (CVE-2021-44228) JNDI lookup protections.
  rule {
    name     = "AWSManagedRulesKnownBadInputsRuleSet"
    priority = 30

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        vendor_name = "AWS"
        name        = "AWSManagedRulesKnownBadInputsRuleSet"
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-AWSManagedRulesKnownBadInputsRuleSet"
      sampled_requests_enabled   = true
    }
  }

  # SizeRestrictions_BODY (8 KB) is counted, not blocked: the API enforces its
  # own 8 KiB limit and answers with a proper 413 problem response.
  rule {
    name     = "AWSManagedRulesCommonRuleSet"
    priority = 40

    override_action {
      none {}
    }

    statement {
      managed_rule_group_statement {
        vendor_name = "AWS"
        name        = "AWSManagedRulesCommonRuleSet"
        rule_action_override {
          name = "SizeRestrictions_BODY"
          action_to_use {
            count {}
          }
        }

      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${var.name_prefix}-AWSManagedRulesCommonRuleSet"
      sampled_requests_enabled   = true
    }
  }

  dynamic "rule" {
    for_each = {
      rate-api      = { priority = 50, limit = var.api_rate_limit_per_minute, negate = false }
      rate-redirect = { priority = 60, limit = var.redirect_rate_limit_per_minute, negate = true }
    }
    content {
      name     = rule.key
      priority = rule.value.priority

      action {
        block {
          custom_response {
            response_code            = 429
            custom_response_body_key = "rate_limited"
            response_header {
              name  = "Retry-After"
              value = "60"
            }
          }
        }
      }

      statement {
        rate_based_statement {
          limit                 = rule.value.limit
          evaluation_window_sec = 60
          aggregate_key_type    = "IP"

          scope_down_statement {
            dynamic "byte_match_statement" {
              for_each = rule.value.negate ? [] : [1]
              content {
                search_string         = "/api/"
                positional_constraint = "STARTS_WITH"
                field_to_match {
                  uri_path {}
                }
                text_transformation {
                  priority = 0
                  type     = "NONE"
                }
              }
            }
            dynamic "not_statement" {
              for_each = rule.value.negate ? [1] : []
              content {
                statement {
                  byte_match_statement {
                    search_string         = "/api/"
                    positional_constraint = "STARTS_WITH"
                    field_to_match {
                      uri_path {}
                    }
                    text_transformation {
                      priority = 0
                      type     = "NONE"
                    }
                  }
                }
              }
            }
          }
        }
      }

      visibility_config {
        cloudwatch_metrics_enabled = true
        metric_name                = "${var.name_prefix}-${rule.key}"
        sampled_requests_enabled   = true
      }
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = var.name_prefix
    sampled_requests_enabled   = true
  }
}

resource "aws_cloudwatch_log_group" "waf" {
  #checkov:skip=CKV_AWS_338:Retention is 14 days (dev) / 90 days (prod) per SPEC §12.6.
  region = local.global_region

  name              = "aws-waf-logs-${var.name_prefix}" # required prefix
  retention_in_days = var.log_retention_days
  kms_key_id        = var.us_east_1_kms_key_arn
}

resource "aws_wafv2_web_acl_logging_configuration" "this" {
  region = local.global_region

  resource_arn            = aws_wafv2_web_acl.this.arn
  log_destination_configs = [aws_cloudwatch_log_group.waf.arn]

  redacted_fields {
    single_header {
      name = "authorization"
    }
  }
}

# --- Optional custom domain ------------------------------------------------------------------

resource "aws_acm_certificate" "viewer" {
  count  = local.custom ? 1 : 0
  region = local.global_region

  domain_name       = var.domain_name
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_acm_certificate" "origin" {
  count = local.custom ? 1 : 0

  domain_name       = "origin.${var.domain_name}"
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

# Keyed by the (statically known) certificate domain names so for_each never
# depends on values that are only known after the certificates exist.
locals {
  validation_records = local.custom ? {
    (var.domain_name) = one([
      for d in aws_acm_certificate.viewer[0].domain_validation_options : { name = d.resource_record_name, type = d.resource_record_type, value = d.resource_record_value }
    ])
    "origin.${var.domain_name}" = one([
      for d in aws_acm_certificate.origin[0].domain_validation_options : { name = d.resource_record_name, type = d.resource_record_type, value = d.resource_record_value }
    ])
  } : {}
}

resource "aws_route53_record" "validation" {
  for_each = local.validation_records

  zone_id         = var.hosted_zone_id
  name            = each.value.name
  type            = each.value.type
  records         = [each.value.value]
  ttl             = 60
  allow_overwrite = true
}

resource "aws_acm_certificate_validation" "viewer" {
  count  = local.custom ? 1 : 0
  region = local.global_region

  certificate_arn         = aws_acm_certificate.viewer[0].arn
  validation_record_fqdns = [aws_route53_record.validation[var.domain_name].fqdn]
}

resource "aws_acm_certificate_validation" "origin" {
  count = local.custom ? 1 : 0

  certificate_arn         = aws_acm_certificate.origin[0].arn
  validation_record_fqdns = [aws_route53_record.validation["origin.${var.domain_name}"].fqdn]
}

# origin.<domain> resolves to the internal ALB (private IPs) so its certificate
# matches the name CloudFront uses for the HTTPS VPC-origin connection.
resource "aws_route53_record" "origin" {
  count = local.custom ? 1 : 0

  zone_id = var.hosted_zone_id
  name    = "origin.${var.domain_name}"
  type    = "A"

  alias {
    name                   = aws_lb.this.dns_name
    zone_id                = aws_lb.this.zone_id
    evaluate_target_health = false
  }
}

resource "aws_route53_record" "viewer" {
  for_each = local.custom ? toset(["A", "AAAA"]) : toset([])

  zone_id = var.hosted_zone_id
  name    = var.domain_name
  type    = each.key

  alias {
    name                   = aws_cloudfront_distribution.this.domain_name
    zone_id                = aws_cloudfront_distribution.this.hosted_zone_id
    evaluate_target_health = false
  }
}

check "custom_domain_inputs" {
  assert {
    condition     = var.domain_name == null || var.hosted_zone_id != null
    error_message = "domain_name requires hosted_zone_id."
  }
}
