# TinyEMU-Go Makefile
# Cross-compilation, testing, linting, and quality checks.

# ------------------------------------------------------------------------------
# Configuration
# ------------------------------------------------------------------------------

BINARY_NAME   := temu
MAIN_PACKAGE  := ./cmd/temu
BUILD_DIR     := bin

# Go build flags for reproducible, stripped binaries
LDFLAGS       := -s -w
GOFLAGS       := -trimpath

# ------------------------------------------------------------------------------
# Derived values
# ------------------------------------------------------------------------------

NATIVE_OS   := $(shell uname -s | tr A-Z a-z)
NATIVE_ARCH := $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_DIRTY   := $(shell git diff --quiet 2>/dev/null && echo clean || echo dirty)
BUILD_TIME  := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Inject minimal build metadata
LDFLAGS += -X main.gitCommit=$(GIT_COMMIT) -X main.buildTime=$(BUILD_TIME)

# ------------------------------------------------------------------------------
# Helpers
# ------------------------------------------------------------------------------

# Print a message in bold
define echo
	@echo "\033[1m>> $(1)\033[0m"
endef

# Check if a command exists
HAVE_CMD = $(shell command -v $(1) >/dev/null 2>&1 && echo yes || echo no)

# ------------------------------------------------------------------------------
# Default target
# ------------------------------------------------------------------------------

.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ------------------------------------------------------------------------------
# Build targets
# ------------------------------------------------------------------------------

.PHONY: all build-all
all: build-linux-amd64 build-linux-arm64 build-windows-amd64 build-darwin-amd64 ## Build all cross-compiled binaries
build-all: all ## Alias for 'all'

.PHONY: build
build: ## Build native binary for current OS/arch
	$(call echo,"Building native binary: $(NATIVE_OS)-$(NATIVE_ARCH)")
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME).$(NATIVE_OS)-$(NATIVE_ARCH).bin $(MAIN_PACKAGE)
	@echo "  -> $(BUILD_DIR)/$(BINARY_NAME).$(NATIVE_OS)-$(NATIVE_ARCH).bin"

.PHONY: asm-lsp
asm-lsp: ## Build the asm language server into bin/asm-lsp
	$(call echo,"Building asm-lsp language server")
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -o $(BUILD_DIR)/asm-lsp ./lsp
	@echo "  -> $(BUILD_DIR)/asm-lsp"

.PHONY: build-linux-amd64
build-linux-amd64: ## Build for linux/amd64
	$(call echo,"Building linux/amd64")
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME).linux-amd64.bin $(MAIN_PACKAGE)
	@echo "  -> $(BUILD_DIR)/$(BINARY_NAME).linux-amd64.bin"

.PHONY: build-linux-arm64
build-linux-arm64: ## Build for linux/arm64
	$(call echo,"Building linux/arm64")
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME).linux-arm64.bin $(MAIN_PACKAGE)
	@echo "  -> $(BUILD_DIR)/$(BINARY_NAME).linux-arm64.bin"

.PHONY: build-windows-amd64
build-windows-amd64: ## Build for windows/amd64
	$(call echo,"Building windows/amd64")
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME).windows-amd64.bin $(MAIN_PACKAGE)
	@echo "  -> $(BUILD_DIR)/$(BINARY_NAME).windows-amd64.bin"

.PHONY: build-darwin-amd64
build-darwin-amd64: ## Build for darwin/amd64
	$(call echo,"Building darwin/amd64")
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME).darwin-amd64.bin $(MAIN_PACKAGE)
	@echo "  -> $(BUILD_DIR)/$(BINARY_NAME).darwin-amd64.bin"

# ------------------------------------------------------------------------------
# Test targets
# ------------------------------------------------------------------------------

.PHONY: test
test: ## Run all unit tests
	$(call echo,"Running tests")
	go test -v -count=1 ./...

.PHONY: test-short
test-short: ## Run short tests only
	$(call echo,"Running short tests")
	go test -short -count=1 ./...

.PHONY: test-race
test-race: ## Run tests with the race detector
	$(call echo,"Running tests with race detector")
	go test -race -count=1 ./...

.PHONY: test-cpu-riscv
test-cpu-riscv: ## Run RISC-V CPU tests
	$(call echo,"Running RISC-V CPU tests")
	go test -v -count=1 ./cpu/riscv/...

.PHONY: test-cpu-x86
test-cpu-x86: ## Run x86 CPU tests
	$(call echo,"Running x86 CPU tests")
	go test -v -count=1 ./cpu/x86/...

.PHONY: test-x86-asm
test-x86-asm: ## Run x86 assembly tests (requires nasm)
	$(call echo,"Running x86 assembly tests")
	@which nasm >/dev/null 2>&1 || { echo "nasm is required but not installed"; exit 1; }
	go test -v -count=1 ./test/x86/...

.PHONY: test-x86-test386
test-x86-test386: ## Run test386 CPU test suite milestones
	$(call echo,"Running test386 milestones")
	go test -v -count=1 -run 'TestTest386Milestone' ./test/x86/...

.PHONY: test-machine
test-machine: ## Run machine tests
	$(call echo,"Running machine tests")
	go test -v -count=1 ./machine/...

# ------------------------------------------------------------------------------
# Coverage
# ------------------------------------------------------------------------------

.PHONY: coverage
coverage: ## Generate test coverage report
	$(call echo,"Generating coverage report")
	go test -count=1 -coverprofile=$(BUILD_DIR)/coverage.out ./...
	go tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html
	@echo "  -> $(BUILD_DIR)/coverage.html"

.PHONY: coverage-summary
coverage-summary: ## Print coverage summary
	$(call echo,"Coverage summary")
	go test -count=1 -coverprofile=$(BUILD_DIR)/coverage.out ./...
	go tool cover -func=$(BUILD_DIR)/coverage.out | tail -1

# ------------------------------------------------------------------------------
# Linting & formatting
# ------------------------------------------------------------------------------

.PHONY: fmt
fmt: ## Format all Go source files
	$(call echo,"Formatting code")
	@find . -path ./vendor -prune -o -name '*.go' -print0 | xargs -0 gofmt -w -s

.PHONY: fmt-check
fmt-check: ## Check if code is formatted
	$(call echo,"Checking code formatting")
	@UNFMT=$$(find . -path ./vendor -prune -o -name '*.go' -print0 | xargs -0 gofmt -l); \
	if [ -n "$$UNFMT" ]; then \
		echo "The following files need formatting:"; \
		echo "$$UNFMT"; \
		exit 1; \
	fi
	@echo "  All files are formatted."

.PHONY: vet
vet: ## Run go vet
	$(call echo,"Running go vet")
	go vet ./...

.PHONY: lint
lint: fmt-check vet ## Run all available linters (fmt-check + vet + optional tools)
	$(call echo,"Running linters")
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "  -> golangci-lint"; \
		golangci-lint run ./...; \
	else \
		echo "  -> golangci-lint not installed (skip: https://golangci-lint.run/usage/install/)"; \
	fi
	@if command -v staticcheck >/dev/null 2>&1; then \
		echo "  -> staticcheck"; \
		staticcheck ./...; \
	else \
		echo "  -> staticcheck not installed (skip: go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
	fi
	@if command -v gosec >/dev/null 2>&1; then \
		echo "  -> gosec"; \
		gosec -quiet ./...; \
	else \
		echo "  -> gosec not installed (skip: go install github.com/securego/gosec/v2/cmd/gosec@latest)"; \
	fi

# ------------------------------------------------------------------------------
# Quality & security checks
# ------------------------------------------------------------------------------

.PHONY: check
check: fmt-check vet test test-race ## Run the full quality check suite
	$(call echo,"Full quality check complete")

.PHONY: check-ci
check-ci: fmt-check vet test ## Run CI-friendly quality checks (no race — slower)
	$(call echo,"CI checks complete")

.PHONY: mod-verify
mod-verify: ## Verify module dependencies
	$(call echo,"Verifying modules")
	go mod verify
	go mod tidy
	@if [ -n "$$(git diff --name-only go.mod go.sum)" ]; then \
		echo "go.mod or go.sum changed after tidy — please commit changes"; \
		exit 1; \
	fi

.PHONY: mod-tidy
mod-tidy: ## Tidy and vendor module dependencies
	$(call echo,"Tidying modules")
	go mod tidy
	go mod vendor

# ------------------------------------------------------------------------------
# Benchmarks
# ------------------------------------------------------------------------------

.PHONY: benchmark
benchmark: ## Run all benchmarks
	$(call echo,"Running benchmarks")
	go test -bench=. -benchmem ./...

.PHONY: benchmark-cpu-riscv
benchmark-cpu-riscv: ## Run RISC-V CPU benchmarks
	$(call echo,"Running RISC-V CPU benchmarks")
	go test -bench=. -benchmem ./cpu/riscv/...

# ------------------------------------------------------------------------------
# Clean
# ------------------------------------------------------------------------------

.PHONY: clean
clean: ## Remove build artifacts
	$(call echo,"Cleaning build artifacts")
	rm -rf $(BUILD_DIR)
	go clean -cache

# ------------------------------------------------------------------------------
# Development helpers
# ------------------------------------------------------------------------------

.PHONY: run
run: build ## Build and run the native binary (pass args with TEMU_ARGS="...")
	$(BUILD_DIR)/$(BINARY_NAME).$(NATIVE_OS)-$(NATIVE_ARCH).bin $(TEMU_ARGS)

.PHONY: install-tools
install-tools: ## Install optional development tools (linters, etc.)
	$(call echo,"Installing development tools")
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "Install golangci-lint manually: https://golangci-lint.run/usage/install/"
