mock_provider "aws" {
  mock_resource "aws_elasticache_serverless_cache" {
    defaults = {
      arn      = "arn:aws:elasticache:us-east-1:123456789012:serverlesscache:shortener-test"
      endpoint = [{ address = "shortener-test-abc123.serverless.use1.cache.amazonaws.com", port = 6379 }]
    }
  }
}

variables {
  name_prefix = "shortener-test"
  kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  vpc_id      = "vpc-0123456789abcdef0"
  subnet_ids  = ["subnet-0aaaaaaaaaaaaaaaa", "subnet-0bbbbbbbbbbbbbbbb"]
}

run "valkey_uses_iam_auth_cmk_and_limits" {
  command = apply

  assert {
    condition     = output.endpoint == "shortener-test-abc123.serverless.use1.cache.amazonaws.com:6379"
    error_message = "CACHE_ADDR output must be host:port."
  }

  assert {
    condition     = aws_elasticache_serverless_cache.this.engine == "valkey" && aws_elasticache_serverless_cache.this.major_engine_version == "9"
    error_message = "Valkey 9 serverless."
  }

  assert {
    condition     = one(aws_elasticache_user.app.authentication_mode).type == "iam" && aws_elasticache_user.app.user_id == aws_elasticache_user.app.user_name
    error_message = "The application user must use IAM authentication with user_id == user_name."
  }

  assert {
    condition     = strcontains(aws_elasticache_user.app.access_string, "-@dangerous")
    error_message = "Dangerous commands must be denied to the application user."
  }

  assert {
    condition     = startswith(aws_elasticache_user.default.access_string, "off")
    error_message = "The mandatory default user must be disabled."
  }

  assert {
    condition     = aws_elasticache_serverless_cache.this.user_group_id == aws_elasticache_user_group.this.user_group_id
    error_message = "The cache must use the IAM user group (no open access)."
  }

  assert {
    condition     = aws_elasticache_serverless_cache.this.kms_key_id == var.kms_key_arn
    error_message = "Data at rest must use the environment CMK."
  }

  assert {
    condition = (
      one(one(aws_elasticache_serverless_cache.this.cache_usage_limits).data_storage).maximum == 1 &&
      one(one(aws_elasticache_serverless_cache.this.cache_usage_limits).ecpu_per_second).maximum == 5000
    )
    error_message = "Storage and ECPU limits must be set (cost caps)."
  }

  assert {
    condition = (
      aws_vpc_security_group_ingress_rule.cache_from_clients.cidr_ipv4 == null &&
      aws_vpc_security_group_ingress_rule.cache_from_clients.referenced_security_group_id == aws_security_group.clients.id
    )
    error_message = "Valkey ingress must come only from the client security group."
  }
}

run "rejects_bad_inputs" {
  command = plan

  variables {
    name_prefix         = "Shortener--Dev"
    subnet_ids          = ["subnet-0aaaaaaaaaaaaaaaa"]
    max_ecpu_per_second = 10
  }

  expect_failures = [var.name_prefix, var.subnet_ids, var.max_ecpu_per_second]
}
