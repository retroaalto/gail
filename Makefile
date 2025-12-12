# Makefile for gail

# Get version from git tags, fallback to 'dev' if no tags exist
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.Version=$(VERSION)

# Binary name
BINARY := gail

# Default target
.PHONY: all
all: build

# Build the binary with version info
.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY)

# Install to GOBIN
.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)"

# Run tests
.PHONY: test
test:
	go test -v ./...

# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(BINARY)

# Show version that would be embedded
.PHONY: version
version:
	@echo "Version: $(VERSION)"

# Help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make build    - Build the binary with version info"
	@echo "  make install  - Install to GOBIN with version info"
	@echo "  make test     - Run tests"
	@echo "  make clean    - Remove build artifacts"
	@echo "  make version  - Show current version"
	@echo "  make help     - Show this help message"
