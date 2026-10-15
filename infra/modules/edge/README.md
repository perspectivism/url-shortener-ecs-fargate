# Edge module

CloudFront + WAF in front of an internal ALB reached through a CloudFront VPC origin ([ADR-0012](../../../docs/adr/0012-cloudfront-vpc-origin-internal-alb-waf.md)): ALB security group admits only the CloudFront origin-facing prefix list, CachingDisabled on both behaviors, a custom origin request policy (Authorization, viewer address/country, ...), managed security headers, WAF managed rules and per-IP rate limits answering 429, logs for CloudFront, WAF and the ALB, and an optional custom domain with ACM certificates and HTTPS end to end.

Tests: `tests/*.tftest.hcl` (`terraform test`, mock provider).

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| terraform | >= 1.16.5, < 2.0.0 |
| aws | >= 6.68.0, < 7.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| aws | >= 6.68.0, < 7.0.0 |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_acm_certificate.origin](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/acm_certificate) | resource |
| [aws_acm_certificate.viewer](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/acm_certificate) | resource |
| [aws_acm_certificate_validation.origin](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/acm_certificate_validation) | resource |
| [aws_acm_certificate_validation.viewer](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/acm_certificate_validation) | resource |
| [aws_cloudfront_distribution.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution) | resource |
| [aws_cloudfront_origin_request_policy.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_origin_request_policy) | resource |
| [aws_cloudfront_vpc_origin.alb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_vpc_origin) | resource |
| [aws_cloudwatch_log_delivery.cloudfront](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_delivery) | resource |
| [aws_cloudwatch_log_delivery_destination.cloudfront](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_delivery_destination) | resource |
| [aws_cloudwatch_log_delivery_source.cloudfront](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_delivery_source) | resource |
| [aws_cloudwatch_log_group.cloudfront](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_cloudwatch_log_group.waf](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_lb.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb) | resource |
| [aws_lb_listener.http](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener) | resource |
| [aws_lb_listener.https](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener) | resource |
| [aws_lb_listener_rule.api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener_rule) | resource |
| [aws_lb_target_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group) | resource |
| [aws_route53_record.origin](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record) | resource |
| [aws_route53_record.validation](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record) | resource |
| [aws_route53_record.viewer](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route53_record) | resource |
| [aws_security_group.alb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_vpc_security_group_egress_rule.alb_to_targets](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_vpc_security_group_ingress_rule.alb_from_cloudfront](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule) | resource |
| [aws_wafv2_web_acl.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl) | resource |
| [aws_wafv2_web_acl_logging_configuration.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/wafv2_web_acl_logging_configuration) | resource |
| [aws_cloudfront_cache_policy.caching_disabled](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/cloudfront_cache_policy) | data source |
| [aws_cloudfront_response_headers_policy.security_headers](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/cloudfront_response_headers_policy) | data source |
| [aws_ec2_managed_prefix_list.cloudfront_origin](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ec2_managed_prefix_list) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| force\_destroy | Disable ALB deletion protection (dev teardown). | `bool` | n/a | yes |
| log\_bucket\_id | Log bucket for ALB access logs (prefix alb/). | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| subnet\_ids | Isolated subnets for the internal ALB (AZs supported by CloudFront VPC origins). | `list(string)` | n/a | yes |
| target\_security\_group\_ids | Task security groups the ALB may reach, keyed by service (redirect, api). | `map(string)` | n/a | yes |
| us\_east\_1\_kms\_key\_arn | CMK in us-east-1 for the CloudFront and WAF log groups. | `string` | n/a | yes |
| vpc\_id | VPC ID. | `string` | n/a | yes |
| anonymous\_ip\_action | AWSManagedRulesAnonymousIpList: "count" (initial) or "block" (after review). | `string` | `"count"` | no |
| api\_rate\_limit\_per\_minute | WAF per-IP limit for /api/* requests per minute. | `number` | `300` | no |
| container\_port | Target port of the services. | `number` | `8080` | no |
| domain\_name | Optional custom domain (e.g. sho.rt). Enables ACM certificates, HTTPS to the origin and TLSv1.2\_2021 for viewers. | `string` | `null` | no |
| hosted\_zone\_id | Route 53 public hosted zone for domain\_name (required with domain\_name). | `string` | `null` | no |
| log\_retention\_days | Retention of the CloudFront and WAF log groups. | `number` | `14` | no |
| redirect\_rate\_limit\_per\_minute | WAF per-IP limit for redirect requests per minute. | `number` | `1200` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| alb\_arn | Internal ALB ARN. |
| alb\_arn\_suffix | ALB ARN suffix (CloudWatch dimension). |
| alb\_dns\_name | Internal ALB DNS name (resolves to private IPs; not reachable from the internet). |
| alb\_security\_group\_id | ALB security group ID. |
| base\_url | Public base URL (BASE\_URL). |
| distribution\_domain\_name | CloudFront domain name. |
| distribution\_id | CloudFront distribution ID. |
| routing\_ready | Per service, the listener (redirect, default action) or listener rule (api) that attaches its target group; services depend on it. |
| target\_group\_arn\_suffixes | Target group ARN suffix per service (CloudWatch dimension). |
| target\_group\_arns | Target group ARN per service. |
| web\_acl\_arn | WAF web ACL ARN. |
| web\_acl\_name | WAF web ACL name (CloudWatch dimension). |
<!-- END_TF_DOCS -->