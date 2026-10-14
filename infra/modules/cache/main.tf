# ElastiCache Serverless for Valkey with IAM authentication (ADR-0008, ADR-0009).
# There is no password anywhere: tasks present a SigV4 token signed with their
# role, authorized by elasticache:Connect on the cache and the app user.

resource "aws_security_group" "cache" {
  name        = "${var.name_prefix}-cache"
  description = "Valkey Serverless: 6379-6380 from cache clients only"
  vpc_id      = var.vpc_id
  tags        = { Name = "${var.name_prefix}-cache" }
}

# Tasks that use Valkey attach this group.
resource "aws_security_group" "clients" {
  #checkov:skip=CKV2_AWS_5:Attached to ECS tasks by the ecs-service module (M5).
  name        = "${var.name_prefix}-cache-clients"
  description = "Members may reach Valkey"
  vpc_id      = var.vpc_id
  tags        = { Name = "${var.name_prefix}-cache-clients" }
}

resource "aws_vpc_security_group_ingress_rule" "cache_from_clients" {
  security_group_id            = aws_security_group.cache.id
  description                  = "Valkey primary (6379) and reader (6380) endpoints from cache clients"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6380
  referenced_security_group_id = aws_security_group.clients.id
}

resource "aws_vpc_security_group_egress_rule" "clients_to_cache" {
  security_group_id            = aws_security_group.clients.id
  description                  = "Valkey primary and reader endpoints"
  ip_protocol                  = "tcp"
  from_port                    = 6379
  to_port                      = 6380
  referenced_security_group_id = aws_security_group.cache.id
}

# Every user group must contain a user named "default"; this one is disabled.
resource "aws_elasticache_user" "default" {
  user_id       = "${var.name_prefix}-default"
  user_name     = "default"
  engine        = "valkey"
  access_string = "off -@all"

  authentication_mode {
    type = "no-password-required"
  }
}

# IAM-authenticated application user: user_id must equal user_name.
resource "aws_elasticache_user" "app" {
  user_id       = "${var.name_prefix}-app"
  user_name     = "${var.name_prefix}-app"
  engine        = "valkey"
  access_string = var.app_access_string

  authentication_mode {
    type = "iam"
  }
}

resource "aws_elasticache_user_group" "this" {
  user_group_id = "${var.name_prefix}-users"
  engine        = "valkey"
  user_ids      = [aws_elasticache_user.default.user_id, aws_elasticache_user.app.user_id]
}

resource "aws_elasticache_serverless_cache" "this" {
  name                     = var.name_prefix
  description              = "URL Shortener cache (${var.name_prefix})"
  engine                   = "valkey"
  major_engine_version     = var.major_engine_version
  kms_key_id               = var.kms_key_arn
  subnet_ids               = var.subnet_ids
  security_group_ids       = [aws_security_group.cache.id]
  user_group_id            = aws_elasticache_user_group.this.user_group_id
  snapshot_retention_limit = 0 # cache is rebuildable from DynamoDB; no snapshots to clean up

  cache_usage_limits {
    data_storage {
      maximum = var.max_data_storage_gb
      unit    = "GB"
    }

    ecpu_per_second {
      maximum = var.max_ecpu_per_second
    }
  }
}
