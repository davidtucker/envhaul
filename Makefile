# Development tasks. Run `make help` for a list.

BINARY   := envhaul
PKG      := github.com/davidtucker/envhaul
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

GOLANGCI_LINT_VERSION := v2.15.0
GORELEASER_VERSION    := v2.19.0
EMULATOR_COMPOSE      := test/integration/compose.yaml

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[38;5;214m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build ./bin/envhaul
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/envhaul

.PHONY: install
install: ## Install envhaul into GOBIN
	CGO_ENABLED=0 go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/envhaul

.PHONY: test
test: ## Run unit and CLI tests with the race detector
	go test -race -count=1 ./...

.PHONY: cover
cover: ## Run tests with coverage and open the HTML report
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

.PHONY: fuzz
fuzz: ## Fuzz the template parser and dotenv round trip (FUZZTIME=30s each)
	go test -run=XXX -fuzz=FuzzParse -fuzztime=$(or $(FUZZTIME),30s) ./internal/template
	go test -run=XXX -fuzz=FuzzRoundTrip -fuzztime=$(or $(FUZZTIME),30s) ./internal/dotenv

.PHONY: emulator-up
emulator-up: ## Start the AWS emulator (moto) for integration tests
	docker compose -f $(EMULATOR_COMPOSE) up -d --wait

.PHONY: emulator-down
emulator-down: ## Stop the AWS emulator
	docker compose -f $(EMULATOR_COMPOSE) down

.PHONY: test-integration
test-integration: ## Run integration tests against the emulator (make emulator-up first)
	go test -tags integration -count=1 -v ./test/integration

.PHONY: lint
lint: ## Run golangci-lint
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

.PHONY: fmt
fmt: ## Format code
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) fmt

.PHONY: tidy
tidy: ## Tidy go.mod and fail if it changed (as CI does)
	go mod tidy
	git diff --exit-code go.mod go.sum

.PHONY: vuln
vuln: ## Scan for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: completions
completions: ## Generate shell completions into ./completions
	./scripts/completions.sh

.PHONY: manpages
manpages: ## Generate man pages into ./manpages
	./scripts/manpages.sh

.PHONY: docs
docs: ## Generate the CLI reference and serve the docs site locally
	go run ./cmd/envhaul gen docs docs/reference/cli
	python3 -m venv .venv && .venv/bin/pip install -q -r docs/requirements.txt
	.venv/bin/mkdocs serve

.PHONY: snapshot
snapshot: ## Build all release artifacts locally without publishing
	go run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) release --snapshot --clean --skip=sign,sbom

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml
	go run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION) check

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist completions manpages coverage.out coverage.html site

.PHONY: demo
demo: build ## Record docs/assets/demo.gif with VHS
	PATH="$(CURDIR)/bin:$$PATH" vhs docs/demo.tape
