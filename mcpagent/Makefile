.PHONY: help lint lint-fix install-linter test build clean install-act test-ci test-ci-job list-ci-jobs proto proto-go proto-ts install-proto-tools

# Default target
help:
	@echo "Available targets:"
	@echo "  make lint        - Run golangci-lint"
	@echo "  make lint-fix     - Run golangci-lint with auto-fix"
	@echo "  make install-linter - Install golangci-lint"
	@echo "  make test        - Run Go tests"
	@echo "  make build       - Build the project"
	@echo "  make clean       - Clean build artifacts"
	@echo ""
	@echo "GitHub Actions (local testing with act):"
	@echo "  make install-act - Install act (tool to run GitHub Actions locally)"
	@echo "  make test-ci     - Run all CI jobs locally"
	@echo "  make test-ci-job JOB=<job-name> - Run a specific CI job (e.g., JOB=lint)"
	@echo "  make list-ci-jobs - List available CI workflows and jobs"

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
	@go build -o bin/mcpagent-test ./cmd/testing

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@go clean ./...

# Install act (tool to run GitHub Actions locally)
install-act:
	@echo "Installing act..."
	@if command -v brew > /dev/null; then \
		brew install act; \
	elif command -v curl > /dev/null; then \
		curl -s https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash; \
	else \
		echo "Please install act manually: https://github.com/nektos/act#installation"; \
		exit 1; \
	fi
	@act --version
	@echo "✓ act installed successfully"
	@echo ""
	@echo "Next steps:"
	@echo "  Run 'make test-ci' to test all CI jobs locally"

# Test CI workflows locally with act
test-ci: install-act
	@echo "Running CI workflow locally with act..."
	@echo "⚠️  Note: Act can be slow on first run (pulling Docker images)"
	@echo "⚠️  Output may not appear immediately - be patient..."
	@act push --container-architecture linux/amd64

# Test a specific CI job locally
test-ci-job: install-act
	@if [ -z "$(JOB)" ]; then \
		echo "❌ Error: JOB parameter is required"; \
		echo "Usage: make test-ci-job JOB=<job-name>"; \
		echo "Available jobs: lint, security-scan, test, build"; \
		exit 1; \
	fi
	@echo "Running CI job '$(JOB)' locally with act..."
	@echo "⚠️  Note: Act can be slow on first run (pulling Docker images)"
	@echo "⚠️  Output may not appear immediately - be patient..."
	@act push -j $(JOB) --container-architecture linux/amd64

# List available workflows and jobs
list-ci-jobs: install-act
	@echo "Available CI workflows and jobs:"
	@act -l

# Install protobuf tools
install-proto-tools:
	@echo "Installing protobuf tools..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "✓ Go protobuf tools installed"
	@if [ -d "sdk-node" ]; then \
		cd sdk-node && npm install ts-proto @grpc/grpc-js --save-dev; \
		echo "✓ TypeScript protobuf tools installed"; \
	fi

# Generate all proto files
proto: proto-go proto-ts
	@echo "✓ All proto files generated"

# Generate Go proto files
proto-go:
	@echo "Generating Go proto files..."
	@mkdir -p grpcserver/pb
	@protoc --proto_path=proto \
		--go_out=grpcserver/pb --go_opt=paths=source_relative \
		--go-grpc_out=grpcserver/pb --go-grpc_opt=paths=source_relative \
		proto/agent.proto
	@echo "✓ Go proto files generated in grpcserver/pb/"

# Generate TypeScript proto files
proto-ts:
	@echo "Generating TypeScript proto files..."
	@if [ -d "sdk-node" ]; then \
		mkdir -p sdk-node/src/generated; \
		cd sdk-node && npx protoc \
			--plugin=./node_modules/.bin/protoc-gen-ts_proto \
			--ts_proto_out=./src/generated \
			--ts_proto_opt=outputServices=grpc-js \
			--ts_proto_opt=esModuleInterop=true \
			--ts_proto_opt=env=node \
			--ts_proto_opt=useOptionals=messages \
			--ts_proto_opt=exportCommonSymbols=false \
			--proto_path=../proto \
			../proto/agent.proto; \
		echo "✓ TypeScript proto files generated in sdk-node/src/generated/"; \
	else \
		echo "⚠️  sdk-node directory not found, skipping TypeScript generation"; \
	fi

