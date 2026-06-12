.DEFAULT_GOAL := build

# Set SHELL to bash and configure options
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

GOCMD     ?= go
BINARY    ?= bin/golinkname
CMD_PKG   := ./cmd/golinkname
PKG       := ./...
COVER_OUT ?= coverage.out

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m  %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the CLI into ./bin/golinkname.
	$(GOCMD) build -o $(BINARY) $(CMD_PKG)

.PHONY: install
install: ## go install the CLI into GOBIN.
	$(GOCMD) install $(CMD_PKG)

.PHONY: test
test: ## Run all tests.
	$(GOCMD) test -v $(PKG)

.PHONY: test-race
test-race: ## Run all tests with the race detector enabled.
	$(GOCMD) test -v -race $(PKG)

.PHONY: cover
cover: ## Run all tests with coverage; writes coverage.out.
	$(GOCMD) test -covermode=atomic -coverprofile=$(COVER_OUT) $(PKG)
	$(GOCMD) tool cover -func=$(COVER_OUT) | tail -1

.PHONY: vet
vet: ## go vet over the tree.
	$(GOCMD) vet $(PKG)

.PHONY: fmt
fmt: ## gofmt -w over the tree.
	gofmt -w .

.PHONY: tidy
tidy: ## go mod tidy.
	$(GOCMD) mod tidy

.PHONY: clean
clean: ## Remove build artefacts.
	rm -rf bin $(COVER_OUT)
