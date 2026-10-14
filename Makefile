# URL Shortener — single entry point for every build, test, scan and deploy step.
# Run `make help` for the list of targets. Tools are pinned in .tool-versions and
# installed into ./.tools by `make tools`.

SHELL := bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-builtin-rules --no-print-directory

ROOT      := $(CURDIR)
TOOLS_BIN := $(ROOT)/.tools/bin
export PATH := $(TOOLS_BIN):$(PATH)
export GOTOOLCHAIN := local
export GOFLAGS := -mod=readonly
export TFLINT_PLUGIN_DIR := $(ROOT)/.tools/tflint-plugins
export PUPPETEER_CACHE_DIR := $(ROOT)/.tools/puppeteer

ENV      ?= dev
GIT_SHA  := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo dev)
COVERAGE_MIN ?= 80

# Fail loudly for targets that belong to a later milestone (never a silent success).
define nyi
	@echo "make $@: not yet implemented ($(1))" >&2; exit 1
endef

##@ General

.PHONY: help
help: ## List targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

.PHONY: tools
tools: ## Verify/install pinned tool versions into ./.tools
	@scripts/install-tools.sh

.PHONY: tools-check
tools-check:
	@scripts/install-tools.sh --check >/dev/null

##@ Local development

COMPOSE  ?= docker compose
# Local targets read .env (copy from .env.example); fall back to the example.
ENV_FILE := $(if $(wildcard .env),.env,.env.example)
WITH_ENV := set -a && . ./$(ENV_FILE) && set +a &&

.PHONY: up
up: ## Start the local compose stack and initialise tables/queues/bucket
	$(COMPOSE) up -d --wait
	@$(WITH_ENV) scripts/wait-for-emulators.sh
	$(WITH_ENV) go run ./cmd/shortenerctl local-init

.PHONY: down
down: ## Stop the local compose stack and discard its data
	$(COMPOSE) down -v --remove-orphans

.PHONY: run-redirect
run-redirect: ## Run the redirect service on :8080 against the local stack
	$(WITH_ENV) HTTP_ADDR=:8080 go run ./cmd/redirect

.PHONY: run-api
run-api: ## Run the API service on :8081 against the local stack
	$(WITH_ENV) HTTP_ADDR=:8081 go run ./cmd/api

.PHONY: apikey
apikey: ## Create a local API key: make apikey OWNER=demo SCOPES=links:read,links:write
	$(WITH_ENV) go run ./cmd/shortenerctl apikey create --owner $(or $(OWNER),demo) --scopes $(or $(SCOPES),links:read,links:write)

##@ Quality

.PHONY: lint
lint: tools-check lint-go lint-md lint-tf lint-actions lint-sh ## golangci-lint, gofmt, markdownlint, tflint, actionlint, shellcheck

.PHONY: lint-go
lint-go:
	golangci-lint fmt --diff
	golangci-lint run ./...

.PHONY: lint-md
lint-md:
	markdownlint-cli2

.PHONY: lint-tf
lint-tf:
	@if [ -d infra ] && find infra -name '*.tf' | grep -q .; then \
		tflint --init >/dev/null && tflint --recursive --config "$(ROOT)/.tflint.hcl"; \
	else echo "tflint: no Terraform files yet"; fi

.PHONY: lint-actions
lint-actions:
	@if find .github/workflows -name '*.yml' 2>/dev/null | grep -q .; then actionlint; \
	else echo "actionlint: no workflows yet"; fi

.PHONY: lint-sh
lint-sh:
	shellcheck scripts/*.sh

.PHONY: test
test: tools-check ## Unit tests with -race and the coverage gate
	@mkdir -p coverage
	go test -race -count=1 -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage/unit.out ./...
	@scripts/check-coverage.sh coverage/unit.out $(COVERAGE_MIN)

.PHONY: test-integration
test-integration: tools-check ## Integration tests against the compose stack (run `make up` first)
	@scripts/wait-for-emulators.sh
	go test -race -count=1 -tags=integration -timeout=15m ./tests/integration/...

.PHONY: test-secret-scan
test-secret-scan: tools-check ## gitleaks self-test (must detect a planted fake key)
	@scripts/test-secret-scan.sh

.PHONY: secret-scan
secret-scan: tools-check ## gitleaks over the working tree and full git history
	gitleaks dir --no-banner --redact .
	@if git rev-parse HEAD >/dev/null 2>&1; then gitleaks git --no-banner --redact .; fi

.PHONY: check-ignore
check-ignore: ## Assert secret-bearing paths are gitignored and examples are not
	@scripts/check-ignore.sh

.PHONY: vuln
vuln: tools-check ## govulncheck
	govulncheck ./...

##@ Containers

.PHONY: images
images: ## Build arm64 images tagged with the git SHA
	$(call nyi,M3)

.PHONY: check-images
check-images: ## Non-root user and size checks
	$(call nyi,M3)

.PHONY: scan-images
scan-images: ## Trivy image + secret scan
	$(call nyi,M3)

##@ Terraform

.PHONY: tf-check
tf-check: ## fmt, validate, tflint, checkov, trivy config, terraform test (all roots/modules)
	$(call nyi,M4)

.PHONY: plan
plan: ## terraform plan -out=tfplan for ENV (read-only)
	$(call nyi,M4)

.PHONY: apply
apply: ## Apply the saved plan for ENV — APPROVAL REQUIRED
	$(call nyi,M4)

.PHONY: bootstrap
bootstrap: ## Plan/apply bootstrap (state bucket, OIDC, CI roles) — APPROVAL REQUIRED
	$(call nyi,M4)

.PHONY: seed-secrets
seed-secrets: ## HUMAN-RUN: prompt for secret values (never echoed) and store them
	$(call nyi,M4)

.PHONY: push-images
push-images: ## Push images to ECR for ENV — APPROVAL REQUIRED
	$(call nyi,M5)

.PHONY: validate-iam
validate-iam: ## IAM Access Analyzer validate-policy over planned policies
	$(call nyi,M7)

##@ Verification

.PHONY: smoke
smoke: ## Smoke tests against BASE_URL
	$(call nyi,M5)

.PHONY: load
load: ## k6 baseline against BASE_URL
	$(call nyi,M6)

.PHONY: docs-check
docs-check: tools-check ## markdownlint, Mermaid render, relative link check
	markdownlint-cli2
	@scripts/check-mermaid.sh
	@scripts/check-links.sh

.PHONY: ci
ci: ## Everything CI runs, locally
	$(call nyi,M8)

##@ Teardown

.PHONY: destroy
destroy: ## Destroy one environment — APPROVAL REQUIRED
	$(call nyi,M4)

.PHONY: teardown
teardown: ## Destroy everything incl. bootstrap state bucket, then verify — APPROVAL REQUIRED
	$(call nyi,M9)

.PHONY: verify-teardown
verify-teardown: ## Report leftover project resources (read-only)
	$(call nyi,M9)
