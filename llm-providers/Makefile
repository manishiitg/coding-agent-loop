.PHONY: help lint lint-fix install-linter test build clean

# Default target
help:
	@echo "Available targets:"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make lint-fix     - Run golangci-lint with auto-fix"
	@echo "  make install-linter - Install golangci-lint"
	@echo "  make test        - Run Go tests"
	@echo "  make build       - Build the project"
	@echo "  make clean       - Clean build artifacts"

# Install golangci-lint
install-linter:
	@echo "Installing golangci-lint..."
	@which golangci-lint > /dev/null || \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
		sh -s -- -b $$(go env GOPATH)/bin latest
	@golangci-lint version
	@echo "✓ golangci-lint installed successfully"

# Run linter
lint: install-linter
	@echo "Running golangci-lint..."
	@golangci-lint run ./...

# Run linter with auto-fix
lint-fix: install-linter
	@echo "Running golangci-lint with auto-fix..."
	@golangci-lint run --fix ./...

# Run tests
test:
	@echo "Running tests..."
	@go test ./... -v

# Build the project
build:
	@echo "Building project..."
	@go build -o bin/llm-test ./cmd/llm-test

# Build chat CLI
build-chat:
	@echo "Building chat CLI..."
	@go build -o bin/llm-chat ./cmd/llm-chat

# Build all binaries
build-all: build build-chat
	@echo "All binaries built successfully"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@go clean ./...

