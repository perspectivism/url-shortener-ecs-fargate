mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "us-east-1" }
  }
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_data "aws_iam_policy" {
    defaults = { arn = "arn:aws:iam::aws:policy/ReadOnlyAccess" }
  }
  mock_resource "aws_kms_key" {
    defaults = { arn = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555" }
  }
  mock_resource "aws_iam_openid_connect_provider" {
    defaults = { arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }
  mock_resource "aws_iam_policy" {
    defaults = { arn = "arn:aws:iam::123456789012:policy/shortener-ci-boundary" }
  }
  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/shortener-ci-x" }
  }
}

variables {
  repository = "acme/url-shortener-ecs-fargate"
  owner      = "platform"
}

run "oidc_trust_is_scoped_per_role" {
  command = apply

  assert {
    condition = alltrue([
      for k, d in data.aws_iam_policy_document.trust :
      anytrue([for c in one(d.statement).condition : c.variable == "token.actions.githubusercontent.com:aud" && contains(c.values, "sts.amazonaws.com")])
    ])
    error_message = "Every CI role must require aud = sts.amazonaws.com."
  }

  assert {
    condition = (
      contains(flatten([for c in one(data.aws_iam_policy_document.trust["plan"].statement).condition : c.values]), "repo:acme/url-shortener-ecs-fargate:pull_request") &&
      contains(flatten([for c in one(data.aws_iam_policy_document.trust["apply-prod"].statement).condition : c.values]), "repo:acme/url-shortener-ecs-fargate:environment:prod")
    )
    error_message = "Plan trusts pull requests; apply roles trust only their GitHub environment."
  }

  assert {
    condition     = alltrue([for r in concat([aws_iam_role.plan], values(aws_iam_role.apply)) : r.permissions_boundary == aws_iam_policy.boundary.arn])
    error_message = "Every CI role must carry the permissions boundary."
  }
}

run "boundary_blocks_escalation_and_long_lived_keys" {
  command = plan

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.boundary.statement :
      s.effect == "Deny" && contains(s.actions, "iam:CreateAccessKey") && contains(s.actions, "iam:CreateUser")
    ])
    error_message = "CI must never create IAM users or access keys."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.boundary.statement :
      s.sid == "RolesMustCarryThisBoundary" && anytrue([for c in s.condition : c.variable == "iam:PermissionsBoundary"])
    ])
    error_message = "Roles created by CI must carry the boundary."
  }

  assert {
    condition     = contains([for s in data.aws_iam_policy_document.boundary.statement : s.sid], "BoundaryPolicyIsImmutable")
    error_message = "CI must not be able to edit its own boundary."
  }
}

run "apply_roles_are_isolated_per_environment" {
  command = plan

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.apply["dev"].statement :
      s.sid == "OnlyOwnState" && contains(s.resources, "arn:aws:s3:::shortener-tfstate-123456789012/envs/prod/*") && !contains(s.resources, "arn:aws:s3:::shortener-tfstate-123456789012/envs/dev/*")
    ])
    error_message = "The dev apply role must be denied prod state (and only prod/bootstrap state)."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.apply["dev"].statement :
      s.sid == "NotOtherEnvironments" && anytrue([for c in s.condition : c.variable == "aws:ResourceTag/Environment" && toset(c.values) == toset(["prod"])])
    ])
    error_message = "The dev apply role must not modify prod-tagged resources."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.plan.statement :
      s.effect == "Deny" && contains(s.actions, "secretsmanager:GetSecretValue") && contains(s.actions, "dynamodb:Scan")
    ])
    error_message = "The plan role must not read application data or secrets."
  }
}

run "state_bucket_is_encrypted_and_kept" {
  command = plan

  assert {
    condition     = module.state_bucket.id == "shortener-tfstate-123456789012"
    error_message = "State bucket name must include the account ID."
  }

  assert {
    condition     = aws_kms_key.state.enable_key_rotation && aws_kms_key.state.deletion_window_in_days == 7
    error_message = "State CMK must rotate; deletion window 7 days."
  }
}

run "existing_oidc_provider_is_reused" {
  command = plan

  variables {
    create_oidc_provider = false
  }

  override_data {
    target = data.aws_iam_openid_connect_provider.github[0]
    values = { arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition     = length(aws_iam_openid_connect_provider.github) == 0
    error_message = "No second provider when one exists."
  }
}
