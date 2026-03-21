.PHONY: all build test lint coverage bench clean help hooks

# Default target
all: lint test build

# Build all packages
build:
	go build ./...

# Build examples
build-examples:
	go build ./examples/...

# Run tests
test:
	go test -race ./...

# Run tests with verbose output
test-v:
	go test -race -v ./...

# Run linter
lint:
	golangci-lint run

# Run linter with auto-fix
lint-fix:
	golangci-lint run --fix

# Generate coverage report
coverage:
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Show coverage in terminal
coverage-text:
	go test -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

# Run benchmarks
bench:
	go test -bench=. -benchmem ./...

# Run benchmarks and save results
bench-save:
	go test -bench=. -benchmem ./... | tee bench.txt

# Tidy dependencies
tidy:
	go mod tidy
	go mod verify

# Download dependencies
deps:
	go mod download

# Clean build artifacts
clean:
	rm -f coverage.out coverage.html bench.txt
	go clean ./...

# Format code
fmt:
	gofmt -s -w .

# Vet code
vet:
	go vet ./...

# Run all checks (for CI)
ci: deps lint test build

# Install git hooks
hooks:
	git config core.hooksPath .githooks
	@echo "Git hooks installed (using .githooks/)"

# Install development tools
tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/pbs/coverctl@latest

# Help
help:
	@echo "Available targets:"
	@echo "  all           - Run lint, test, and build (default)"
	@echo "  build         - Build all packages"
	@echo "  build-examples- Build example applications"
	@echo "  test          - Run tests with race detection"
	@echo "  test-v        - Run tests with verbose output"
	@echo "  lint          - Run golangci-lint"
	@echo "  lint-fix      - Run golangci-lint with auto-fix"
	@echo "  coverage      - Generate HTML coverage report"
	@echo "  coverage-text - Show coverage in terminal"
	@echo "  bench         - Run benchmarks"
	@echo "  bench-save    - Run benchmarks and save to file"
	@echo "  tidy          - Tidy and verify dependencies"
	@echo "  deps          - Download dependencies"
	@echo "  clean         - Remove build artifacts"
	@echo "  fmt           - Format code with gofmt"
	@echo "  vet           - Run go vet"
	@echo "  ci            - Run all CI checks"
	@echo "  hooks         - Install git pre-commit hooks"
	@echo "  tools         - Install development tools"
	@echo "  help          - Show this help"
