mock_provider "aws" {
  mock_data "aws_ec2_managed_prefix_list" {
    defaults = { id = "pl-3b927c52" }
  }
  mock_data "aws_cloudfront_cache_policy" {
    defaults = { id = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad" }
  }
  mock_data "aws_cloudfront_response_headers_policy" {
    defaults = { id = "67f7725c-6f97-4210-82d7-5512b31e9d03" }
  }
  mock_resource "aws_lb" {
    defaults = {
      arn        = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/shortener-test/0123456789abcdef"
      dns_name   = "internal-shortener-test-123.us-east-1.elb.amazonaws.com"
      arn_suffix = "app/shortener-test/0123456789abcdef"
    }
  }
  mock_resource "aws_lb_target_group" {
    defaults = { arn = "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/shortener-test-x/0123456789abcdef" }
  }
  mock_resource "aws_lb_listener" {
    defaults = { arn = "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/shortener-test/0123456789abcdef/0123456789abcdef" }
  }
  mock_resource "aws_cloudfront_distribution" {
    defaults = {
      arn         = "arn:aws:cloudfront::123456789012:distribution/E1234567890ABC"
      domain_name = "d1234567890abc.cloudfront.net"
    }
  }
  mock_resource "aws_wafv2_web_acl" {
    defaults = { arn = "arn:aws:wafv2:us-east-1:123456789012:global/webacl/shortener-test/11111111-2222-3333-4444-555555555555" }
  }
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:aws-waf-logs-shortener-test" }
  }
  mock_resource "aws_cloudwatch_log_delivery_destination" {
    defaults = { arn = "arn:aws:logs:us-east-1:123456789012:delivery-destination:shortener-test-cloudfront" }
  }
}

variables {
  name_prefix               = "shortener-test"
  vpc_id                    = "vpc-0123456789abcdef0"
  subnet_ids                = ["subnet-0aaaaaaaaaaaaaaaa", "subnet-0bbbbbbbbbbbbbbbb"]
  target_security_group_ids = { redirect = "sg-0aaaaaaaaaaaaaaaa", api = "sg-0bbbbbbbbbbbbbbbb" }
  log_bucket_id             = "shortener-test-logs-123456789012"
  us_east_1_kms_key_arn     = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  force_destroy             = true
}

run "alb_is_internal_and_only_cloudfront_reaches_it" {
  command = apply

  assert {
    condition     = aws_lb.this.internal && aws_lb.this.drop_invalid_header_fields
    error_message = "The ALB must be internal and drop invalid header fields."
  }

  assert {
    condition = (
      aws_vpc_security_group_ingress_rule.alb_from_cloudfront.prefix_list_id == "pl-3b927c52" &&
      aws_vpc_security_group_ingress_rule.alb_from_cloudfront.cidr_ipv4 == null &&
      aws_vpc_security_group_ingress_rule.alb_from_cloudfront.referenced_security_group_id == null
    )
    error_message = "ALB ingress must be the CloudFront origin-facing prefix list only."
  }

  assert {
    condition     = toset([for r in aws_vpc_security_group_egress_rule.alb_to_targets : r.referenced_security_group_id]) == toset(["sg-0aaaaaaaaaaaaaaaa", "sg-0bbbbbbbbbbbbbbbb"])
    error_message = "ALB egress must target only the task security groups."
  }

  assert {
    condition     = one(aws_lb.this.access_logs).enabled && one(aws_lb.this.access_logs).prefix == "alb"
    error_message = "ALB access logs are required."
  }

  assert {
    condition     = alltrue([for tg in aws_lb_target_group.this : tg.deregistration_delay == "15" && one(tg.health_check).path == "/healthz" && tg.target_type == "ip"])
    error_message = "Target groups: ip targets, /healthz checks, 15 s deregistration delay."
  }

  assert {
    condition     = one(one(aws_lb_listener_rule.api.condition).path_pattern).values == toset(["/api/*"])
    error_message = "/api/* must route to the api target group."
  }
}

run "cloudfront_never_caches_and_waf_is_attached" {
  command = apply

  assert {
    condition = (
      data.aws_cloudfront_cache_policy.caching_disabled.name == "Managed-CachingDisabled" &&
      one(aws_cloudfront_distribution.this.default_cache_behavior).cache_policy_id == data.aws_cloudfront_cache_policy.caching_disabled.id &&
      length(aws_cloudfront_distribution.this.ordered_cache_behavior) == 1 &&
      alltrue([for b in aws_cloudfront_distribution.this.ordered_cache_behavior : b.cache_policy_id == data.aws_cloudfront_cache_policy.caching_disabled.id])
    )
    error_message = "Both behaviors must use the managed CachingDisabled policy."
  }

  assert {
    condition     = aws_cloudfront_distribution.this.web_acl_id == aws_wafv2_web_acl.this.arn
    error_message = "The WAF web ACL must be associated with the distribution."
  }

  assert {
    condition     = length(one(aws_cloudfront_distribution.this.origin).vpc_origin_config) == 1
    error_message = "The origin must be a CloudFront VPC origin (no public endpoint)."
  }

  assert {
    condition = alltrue([
      for h in ["Authorization", "Idempotency-Key", "CloudFront-Viewer-Address", "CloudFront-Viewer-Country", "User-Agent", "Referer", "Content-Type"] :
      contains(one(one(aws_cloudfront_origin_request_policy.this.headers_config).headers).items, h)
    ])
    error_message = "The origin request policy must forward the headers the services need."
  }

  assert {
    condition     = one(aws_cloudfront_distribution.this.default_cache_behavior).viewer_protocol_policy == "redirect-to-https" && aws_cloudfront_distribution.this.price_class == "PriceClass_100" && aws_cloudfront_distribution.this.http_version == "http2and3"
    error_message = "HTTPS only, PriceClass_100, HTTP/2 and HTTP/3."
  }
}

run "waf_rules_and_logging" {
  command = apply

  assert {
    condition = toset([for r in aws_wafv2_web_acl.this.rule : r.name]) == toset([
      "AWSManagedRulesAmazonIpReputationList", "AWSManagedRulesAnonymousIpList", "AWSManagedRulesKnownBadInputsRuleSet",
      "AWSManagedRulesCommonRuleSet", "rate-api", "rate-redirect",
    ])
    error_message = "Four managed rule groups plus two rate-based rules."
  }

  assert {
    condition     = anytrue([for r in aws_wafv2_web_acl.this.rule : r.name == "AWSManagedRulesAnonymousIpList" && length(one(r.override_action).count) == 1])
    error_message = "AnonymousIpList starts in count mode."
  }

  assert {
    condition     = alltrue([for r in aws_wafv2_web_acl.this.rule : one(one(one(r.action).block).custom_response).response_code == 429 if startswith(r.name, "rate-")])
    error_message = "Rate rules must answer 429."
  }

  assert {
    condition = (
      max([for r in aws_wafv2_web_acl.this.rule : r.priority if startswith(r.name, "rate-")]...) <
      min([for r in aws_wafv2_web_acl.this.rule : r.priority if !startswith(r.name, "rate-")]...)
    )
    error_message = "Rate rules must be evaluated before the managed rule groups so they count every request."
  }

  assert {
    condition = anytrue([
      for r in aws_wafv2_web_acl.this.rule : r.name == "rate-api" &&
      one(one(one(r.action).block).custom_response).response_header == toset([{ name = "Retry-After", value = "60" }])
    ])
    error_message = "Rate-limited responses must carry Retry-After."
  }

  assert {
    condition     = one(one(aws_wafv2_web_acl_logging_configuration.this.redacted_fields).single_header).name == "authorization"
    error_message = "WAF logs must redact the Authorization header."
  }

  assert {
    condition     = startswith(aws_cloudwatch_log_group.waf.name, "aws-waf-logs-") && aws_cloudwatch_log_group.waf.kms_key_id == var.us_east_1_kms_key_arn
    error_message = "WAF log group: required prefix, CMK encryption."
  }
}

run "custom_domain_enables_tls_everywhere" {
  command = plan

  variables {
    domain_name    = "sho.rt.example"
    hosted_zone_id = "Z0123456789ABCDEFGHIJ"
  }

  assert {
    condition     = length(aws_lb_listener.https) == 1 && length(aws_lb_listener.http) == 0 && aws_lb_listener.https[0].ssl_policy == "ELBSecurityPolicy-TLS13-1-2-2021-06"
    error_message = "With a domain the ALB listens on HTTPS only."
  }

  assert {
    condition     = one(aws_cloudfront_vpc_origin.alb.vpc_origin_endpoint_config).origin_protocol_policy == "https-only"
    error_message = "With a domain the CloudFront-to-ALB hop is HTTPS."
  }

  assert {
    condition     = one(aws_cloudfront_distribution.this.viewer_certificate).minimum_protocol_version == "TLSv1.2_2021"
    error_message = "Viewer TLS >= TLSv1.2_2021 with a custom certificate."
  }

  assert {
    condition     = aws_vpc_security_group_ingress_rule.alb_from_cloudfront.from_port == 443
    error_message = "ALB ingress on 443 with HTTPS."
  }
}

run "rejects_bad_rate_limits" {
  command = plan

  variables {
    api_rate_limit_per_minute = 5
  }

  expect_failures = [var.api_rate_limit_per_minute]
}
