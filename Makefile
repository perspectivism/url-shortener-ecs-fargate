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
GIT_SHA  := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo dev)$(shell git status --porcelain 2>/dev/null | grep -q . && echo -dirty)
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
up: images-local ## Build local images and start the full stack: emulators + redirect :8080, api :8081, worker
	$(COMPOSE) --profile app up -d --wait
	@echo "redirect: http://localhost:8080   api: http://localhost:8081   (make apikey OWNER=demo)"

.PHONY: up-deps
up-deps: ## Start only the emulators and initialise tables/queues/bucket (for go run / tests)
	$(COMPOSE) up -d --wait dynamodb valkey sqs s3
	@$(WITH_ENV) scripts/wait-for-emulators.sh
	$(WITH_ENV) go run ./cmd/shortenerctl local-init

.PHONY: down
down: ## Stop the local compose stack and discard its data
	$(COMPOSE) --profile app --profile jobs down -v --remove-orphans

.PHONY: run-redirect
run-redirect: ## Run the redirect service on :8080 against the local stack
	$(WITH_ENV) HTTP_ADDR=:8080 go run ./cmd/redirect

.PHONY: run-api
run-api: ## Run the API service on :8081 against the local stack
	$(WITH_ENV) HTTP_ADDR=:8081 go run ./cmd/api

OWNER  ?= demo
SCOPES ?= links:read,links:write

.PHONY: apikey
apikey: ## Create a local API key: make apikey OWNER=demo SCOPES=links:read,links:write
	@$(COMPOSE) --profile app run --rm --no-deps init apikey create --owner $(OWNER) --scopes $(SCOPES)

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
test-integration: tools-check ## Integration tests: services as host processes against the emulators
	@scripts/wait-for-emulators.sh
	go test -race -count=1 -tags=integration -timeout=15m ./tests/integration/...

.PHONY: test-integration-containers
test-integration-containers: tools-check images-local ## Integration tests with every service in a read-only, cap-dropped, non-root container
	@scripts/wait-for-emulators.sh
	IT_MODE=container go test -count=1 -tags=integration -timeout=20m ./tests/integration/...

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

SERVICES   := redirect api worker nightly
IMAGE_REPO ?= shortener
IMAGE_TAG  ?= $(GIT_SHA)
PLATFORM   ?= linux/arm64
export IMAGE_TAG

.PHONY: images
images: ## Build arm64 images tagged with the git SHA
	@for s in $(SERVICES); do \
		echo "==> $(IMAGE_REPO)/$$s:$(IMAGE_TAG) ($(PLATFORM))"; \
		docker buildx build --quiet --platform $(PLATFORM) --build-arg SERVICE=$$s --build-arg VERSION=$(IMAGE_TAG) \
			-f build/Dockerfile -t $(IMAGE_REPO)/$$s:$(IMAGE_TAG) --load . >/dev/null || exit 1; \
	done

.PHONY: images-local
images-local: ## Build native-platform images tagged "local" for the compose app stack
	@for s in $(SERVICES) shortenerctl; do \
		echo "==> $(IMAGE_REPO)/$$s:local"; \
		docker buildx build --quiet --build-arg SERVICE=$$s --build-arg VERSION=$(GIT_SHA) \
			-f build/Dockerfile -t $(IMAGE_REPO)/$$s:local --load . >/dev/null || exit 1; \
	done

.PHONY: check-images
check-images: ## Non-root user, arm64, entrypoint and size (< 30 MB) checks
	@scripts/check-images.sh $(IMAGE_REPO) $(IMAGE_TAG) $(SERVICES)

.PHONY: test-image-secret-scan
test-image-secret-scan: tools-check ## Trivy self-test: a planted fake key in a throwaway image must be detected
	@scripts/test-image-secret-scan.sh

.PHONY: scan-images
scan-images: tools-check ## Trivy vulnerability + secret scan and build-history review
	@scripts/scan-images.sh $(IMAGE_REPO) $(IMAGE_TAG) $(SERVICES)

##@ Terraform

.PHONY: tf-check
tf-check: tools-check ## fmt, validate, tflint, checkov, trivy config, terraform test (all roots/modules)
	@scripts/tf-check.sh

.PHONY: tf-docs
tf-docs: tools-check ## Regenerate module/root READMEs with terraform-docs
	@scripts/tf-docs.sh

.PHONY: plan
plan: tools-check ## terraform plan -out=tfplan for ENV=dev|prod|bootstrap (read-only)
	@scripts/tf-plan.sh $(ENV)

.PHONY: apply
apply: tools-check ## Apply the saved plan for ENV — APPROVAL REQUIRED (APPROVED=yes)
	@scripts/tf-apply.sh $(ENV)

.PHONY: bootstrap
bootstrap: tools-check ## Plan bootstrap; then APPROVED=yes make bootstrap-apply — APPROVAL REQUIRED
	@scripts/bootstrap.sh plan

.PHONY: bootstrap-apply
bootstrap-apply: tools-check ## Apply the saved bootstrap plan and migrate its state — APPROVAL REQUIRED
	@scripts/bootstrap.sh apply

.PHONY: seed-secrets
seed-secrets: ## HUMAN-RUN: prompt for secret values (never echoed) and store them
	@scripts/seed-secrets.sh $(ENV)

.PHONY: push-images
push-images: ## Push the commit's arm64 images to ECR for ENV — APPROVAL REQUIRED (APPROVED=yes)
	@scripts/push-images.sh $(ENV)

.PHONY: validate-iam
validate-iam: tools-check ## IAM Access Analyzer validate-policy over every policy in ENV's saved plan (make plan first; read-only)
	@f=infra/$(if $(filter bootstrap,$(ENV)),bootstrap,envs/$(ENV))/tfplan.json; \
	test -f "$$f" || { echo "no $$f: run make plan ENV=$(ENV) first" >&2; exit 2; }; \
	scripts/validate-iam.sh "$$f" $(if $(ALLOW_UNKNOWN),--allow-unknown)

##@ Verification

.PHONY: smoke
smoke: tools-check ## Smoke tests against BASE_URL (SMOKE_API_KEY from your environment, never a file)
	@test -n "$(BASE_URL)" || { echo "usage: make smoke BASE_URL=https://<distribution> (export SMOKE_API_KEY first)" >&2; exit 2; }
	SMOKE_BASE_URL=$(BASE_URL) go test -count=1 -tags=smoke -timeout=10m -v ./tests/smoke/...

.PHONY: load
load: tools-check ## k6 baseline against BASE_URL (export LOAD_API_KEY first; RATE, DURATION optional)
	@test -n "$(BASE_URL)" || { echo "usage: make load BASE_URL=https://<distribution> [API_URL=…] (export LOAD_API_KEY first)" >&2; exit 2; }
	@test -n "$$LOAD_API_KEY" || { echo "export LOAD_API_KEY (links:read, links:write) — never pass it on the command line" >&2; exit 2; }
	@mkdir -p tests/load/results
	BASE_URL=$(BASE_URL) API_URL=$(or $(API_URL),$(BASE_URL)) \
		k6 run --summary-export=tests/load/results/summary.json tests/load/redirect.js

.PHONY: docs-check
docs-check: tools-check ## markdownlint, Mermaid render, relative link check
	markdownlint-cli2
	@scripts/check-mermaid.sh
	@scripts/check-links.sh
	@python3 -I scripts/check_threat_model.py

.PHONY: ci
ci: ## Everything CI runs, locally
	$(call nyi,M8)

##@ Teardown

.PHONY: destroy
destroy: ## Destroy one environment — APPROVAL REQUIRED
	$(call nyi,M9)

.PHONY: teardown
teardown: ## Destroy everything incl. bootstrap state bucket, then verify — APPROVAL REQUIRED
	$(call nyi,M9)

.PHONY: verify-teardown
verify-teardown: ## Report leftover project resources (read-only)
	$(call nyi,M9)
