# prod settings, versioned (ADR-0028): every plan uses this file, locally and in
# CI, so a deploy never falls back to defaults. Override variable defaults
# here (custom domain, capacity, budgets, ...) and commit the change.
# Not here: secrets (never in Terraform), alert_email (gitignored
# terraform.tfvars locally; GitHub variable ALERT_EMAIL in CI) and image_tag /
# enable_services (set per run). scripts/tf-varfiles.sh rejects alert_email and image_tag.
repository = "perspectivism/url-shortener-ecs-fargate"
owner      = "perspectivism"

# monthly_budget_usd = 150
