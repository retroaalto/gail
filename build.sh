#!/bin/bash
# build.sh - Build script for gail

set -e

VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS="-X main.Version=$VERSION"
BINARY="gail"

case "${1:-build}" in
    build)
        echo "Building $BINARY version $VERSION..."
        go build -ldflags "$LDFLAGS" -o "$BINARY"
        echo "✓ Built $BINARY version $VERSION"
        ;;
    install)
        echo "Installing $BINARY version $VERSION..."
        go install -ldflags "$LDFLAGS"
        echo "✓ Installed $BINARY version $VERSION to $(go env GOPATH)/bin"
        ;;
    clean)
        echo "Cleaning build artifacts..."
        rm -f "$BINARY"
        echo "✓ Cleaned"
        ;;
    version)
        echo "$VERSION"
        ;;
    help|--help|-h)
        echo "Usage: $0 [COMMAND]"
        echo ""
        echo "Commands:"
        echo "  build    Build the binary with version info (default)"
        echo "  install  Install to GOBIN with version info"
        echo "  clean    Remove build artifacts"
        echo "  version  Show current version from git"
        echo "  help     Show this help message"
        ;;
    *)
        echo "Error: Unknown command '$1'"
        echo "Run '$0 help' for usage information"
        exit 1
        ;;
esac
