SHELL := /bin/bash
VERSION := $(shell cat VERSION)
REGISTRY := docker.io/lazarus-ai-research

.PHONY: help build test test-go test-evals images compose-validate clean

help: ## Show available targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F ':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

build: ## Build Go services
	cd control && go build ./...
	cd docker-proxy && go build ./...

test: test-go test-evals ## Run all tests

test-go: ## Run Go tests
	cd control && go vet ./... && go test ./...
	cd docker-proxy && go vet ./... && go test ./...

test-evals: ## Run evals tests
	cd evals && python3 -m pytest tests

images: ## Build application images (context: repo root)
	docker build -f control/Dockerfile -t $(REGISTRY)/sovereign-control:$(VERSION) .
	docker build -f docker-proxy/Dockerfile -t $(REGISTRY)/sovereign-docker-proxy:$(VERSION) .
	docker build -f evals/Dockerfile -t $(REGISTRY)/sovereign-evals:$(VERSION) .

compose-validate: ## Validate the Compose configuration
	@test -f deploy/.env || cp deploy/.env.example deploy/.env
	docker compose --project-directory deploy -f deploy/compose/compose.yml config -q
	@echo "compose configuration valid"

clean: ## Remove build artifacts
	rm -rf bin control/web/dist evals/dist evals/build
