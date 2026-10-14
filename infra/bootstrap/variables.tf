variable "project" {
  description = "Project name: resource prefix and Project tag."
  type        = string
  default     = "shortener"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]{2,15}$", var.project))
    error_message = "project must be 3-16 lowercase letters/digits."
  }
}

variable "region" {
  description = "Region of the state bucket and keys."
  type        = string
  default     = "us-east-1"
}

variable "repository" {
  description = "GitHub repository (owner/name) whose workflows may assume the CI roles."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.repository))
    error_message = "repository must be owner/name."
  }
}

variable "owner" {
  description = "Owner tag value."
  type        = string
}

variable "environments" {
  description = "Environments that get an apply role (GitHub environment names)."
  type        = set(string)
  default     = ["dev", "prod"]
}

variable "create_oidc_provider" {
  description = "Create the GitHub OIDC provider (false if the account already has one for token.actions.githubusercontent.com)."
  type        = bool
  default     = true
}

variable "force_destroy" {
  description = "Allow destroying the state bucket with its object versions (set true only for full teardown)."
  type        = bool
  default     = false
}

variable "activate_cost_allocation_tag" {
  description = "Activate the CostCenter cost allocation tag. Set to true once Billing has seen the tag (up to 24 hours after the first tagged resources exist)."
  type        = bool
  default     = false
}
