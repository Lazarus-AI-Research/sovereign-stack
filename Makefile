SHELL := /bin/bash
VERSION := $(shell cat VERSION)
REGISTRY := ghcr.io/lazarus-ai-research

.PHONY: help build web test test-go test-evals test-contracts test-scripts validate images compose-validate clean

help: ## Show available targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

build: ## Build Go services
	cd control && go build ./...
	cd docker-proxy && go build ./...

web: ## Build the Control UI into control/internal/web/dist
	cd control/web && npm install --no-fund --no-audit && npm run build

test: test-go test-evals test-contracts test-scripts ## Run all tests

test-go: ## Run Go tests
	cd control && go vet ./... && go test ./...
	cd docker-proxy && go vet ./... && go test ./...

test-evals: ## Run evals tests
	cd evals && python3 -m pytest tests

test-contracts: ## Validate schemas and checked-in configuration contracts
	python3 release/validate_contracts.py

test-scripts: ## Parse every shipped shell entrypoint
	bash -n deploy/scripts/*.sh deploy/scripts/sovereign
	chmod +x tests/fixtures/bin/* tests/scripts/*.sh
	tests/scripts/release-artifacts.sh
	tests/scripts/install-lifecycle.sh

validate: test web compose-validate ## Run the local release contract gates

images: ## Build application images (context: repo root)
	docker build --build-arg VERSION=$(VERSION) -f control/Dockerfile -t $(REGISTRY)/sovereign-control:$(VERSION) .
	docker build --build-arg VERSION=$(VERSION) -f docker-proxy/Dockerfile -t $(REGISTRY)/sovereign-docker-proxy:$(VERSION) .
	docker build -f evals/Dockerfile -t $(REGISTRY)/sovereign-evals:$(VERSION) .
	docker build --build-arg VERSION=$(VERSION) -f workspace/Dockerfile -t $(REGISTRY)/sovereign-workspace:$(VERSION) .
	docker build -f embeddinggemma/Dockerfile.cuda -t $(REGISTRY)/sovereign-embeddings:$(VERSION) .

compose-validate: ## Validate the Compose configuration
	SOVEREIGN_ENV_FILE=.env.example docker compose --env-file deploy/.env.example --project-directory deploy -f deploy/compose/compose.yml config -q
	SOVEREIGN_ENV_FILE=.env.example docker compose --env-file deploy/.env.example --project-directory deploy -f deploy/compose/compose.yml -f deploy/compose/compose.runtime.cuda.yml config -q
	SOVEREIGN_ENV_FILE=.env.example docker compose --env-file deploy/.env.example --project-directory deploy -f deploy/compose/compose.yml -f deploy/compose/compose.runtime.metal.yml config -q
	@echo "compose configuration valid"

clean: ## Remove build artifacts
	rm -rf bin control/web/dist evals/dist evals/build
