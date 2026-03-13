# ZSVO Package Manager - Linux Build System
# Supports Linux x86_64, ARM64, i386, ARM

.PHONY: help build clean test lint fmt install uninstall release docker

# Default target
help: ## Show this help message
	@echo 'ZSVO Package Manager - Linux Build System'
	@echo ''
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Variables
BINARY_NAME=zsvo
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Linux platforms
PLATFORMS=linux/amd64 linux/arm64 linux/386 linux/arm

# Build targets
build: ## Build for current platform
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) .

build-all: ## Build for all Linux platforms
	@echo "Building ZSVO for Linux platforms..."
	@mkdir -p bin
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		output_name=$(BINARY_NAME)-$$arch; \
		echo "Building $$arch..."; \
		GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o dist/$$arch/$$output_name .; \
	done
	@echo "Build complete! Binaries are in dist/"

# Platform-specific builds
build-amd64: ## Build for Linux x86_64
	@echo "Building amd64..."
	@mkdir -p dist/amd64
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/amd64/$(BINARY_NAME) .

build-arm64: ## Build for Linux ARM64
	@echo "Building arm64..."
	@mkdir -p dist/arm64
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/arm64/$(BINARY_NAME) .

build-386: ## Build for Linux i386
	@echo "Building 386..."
	@mkdir -p dist/386
	GOOS=linux GOARCH=386 go build $(LDFLAGS) -o dist/386/$(BINARY_NAME) .

build-arm: ## Build for Linux ARM
	@echo "Building arm..."
	@mkdir -p dist/arm
	GOOS=linux GOARCH=arm go build $(LDFLAGS) -o dist/arm/$(BINARY_NAME) .

# Development targets
test: ## Run all tests
	go test -v ./pkg/...

test-race: ## Run tests with race detector
	go test -race -v ./pkg/...

test-coverage: ## Run tests with coverage
	go test -coverprofile=coverage.out ./pkg/...
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run linter
	golangci-lint run

fmt: ## Format code
	go fmt ./...
	goimports -w .

vet: ## Run go vet
	go vet ./...

# Installation
install: ## Install for current platform
	go install $(LDFLAGS) .

uninstall: ## Uninstall
	go clean -i

# Distribution
release: clean build-all ## Create release packages
	@echo "Creating release packages..."
	@mkdir -p release
	@for arch in amd64 arm64 386 arm; do \
		binary_name=$(BINARY_NAME)-$$arch; \
		release_name=$(BINARY_NAME)-$(VERSION)-$$arch; \
		release_dir=release/$$release_name; \
		mkdir -p $$release_dir; \
		cp dist/$$arch/$$binary_name $$release_dir/$(BINARY_NAME); \
		cp README.md $$release_dir/ 2>/dev/null || true; \
		cp LICENSE $$release_dir/ 2>/dev/null || true; \
		cd release && tar -czf $$release_name.tar.gz $$release_name && cd ..; \
		echo "Created $$release_name.tar.gz"; \
	done

# Docker
docker-build: ## Build Docker image
	docker build -t zsvo:$(VERSION) .

docker-run: ## Run Docker container
	docker run -it --rm zsvo:$(VERSION)

# Cleanup
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf dist/
	rm -rf release/
	rm -f coverage.out coverage.html
	go clean -cache

# Development helpers
dev-setup: ## Setup development environment
	@echo "Setting up development environment..."
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

pre-commit: fmt vet lint test ## Run pre-commit checks

# Quick build for current platform
quick: ## Quick build for current platform (no optimizations)
	go build -o bin/$(BINARY_NAME) .

# Optimized build
optimized: ## Build with optimizations for current platform
	go build -ldflags "-s -w" -o bin/$(BINARY_NAME) .

# Check for required tools
check-tools:
	@echo "Checking for required tools..."
	@command -v go >/dev/null 2>&1 || { echo "Go is required but not installed."; exit 1; }
	@echo "All required tools are installed."

# Show build info
info: ## Show build information
	@echo "Binary: $(BINARY_NAME)"
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Go Version: $(shell go version)"
	@echo "Supported Platforms: $(PLATFORMS)"
