# Two tables (ADR-0005). Definitions must match internal/store/schema.go,
# which creates the same tables in DynamoDB Local.

resource "aws_dynamodb_table" "links" {
  name                        = "${var.name_prefix}-links"
  billing_mode                = "PAY_PER_REQUEST"
  hash_key                    = "pk"
  range_key                   = "sk"
  deletion_protection_enabled = var.deletion_protection

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  attribute {
    name = "gsi1pk"
    type = "S"
  }

  attribute {
    name = "gsi1sk"
    type = "S"
  }

  global_secondary_index {
    name               = "gsi1"
    projection_type    = "INCLUDE"
    non_key_attributes = ["long_url", "status", "created_at", "expires_at"]

    key_schema {
      attribute_name = "gsi1pk"
      key_type       = "HASH"
    }

    key_schema {
      attribute_name = "gsi1sk"
      key_type       = "RANGE"
    }

    on_demand_throughput {
      max_read_request_units  = var.links_max_read_units
      max_write_request_units = var.links_max_write_units
    }
  }

  on_demand_throughput {
    max_read_request_units  = var.links_max_read_units
    max_write_request_units = var.links_max_write_units
  }

  point_in_time_recovery {
    enabled = true
  }

  server_side_encryption {
    enabled     = true
    kms_key_arn = var.kms_key_arn
  }

  ttl {
    attribute_name = "ttl"
    enabled        = true
  }
}

resource "aws_dynamodb_table" "analytics" {
  name                        = "${var.name_prefix}-analytics"
  billing_mode                = "PAY_PER_REQUEST"
  hash_key                    = "pk"
  range_key                   = "sk"
  deletion_protection_enabled = var.deletion_protection

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  on_demand_throughput {
    max_read_request_units  = var.analytics_max_read_units
    max_write_request_units = var.analytics_max_write_units
  }

  point_in_time_recovery {
    enabled = true
  }

  server_side_encryption {
    enabled     = true
    kms_key_arn = var.kms_key_arn
  }

  ttl {
    attribute_name = "ttl"
    enabled        = true
  }
}
