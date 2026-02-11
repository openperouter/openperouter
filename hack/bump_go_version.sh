#!/bin/bash

set -e


GO_VERSION=${1:-}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -z "$GO_VERSION" ]; then
    echo "No version specified, fetching latest Go version..."
    echo ""

    FULL_VERSION=$(curl -s "https://go.dev/VERSION?m=text" | head -1)

    if [ -z "$FULL_VERSION" ]; then
        echo "Error: Failed to fetch latest Go version"
        echo "Please specify a version manually:"
        echo "Usage: $0 <go-version>"
        echo "Example: $0 1.25.7"
        exit 1
    fi

    GO_VERSION=${FULL_VERSION#go}
    echo "Latest Go version found: $GO_VERSION"
fi

if ! [[ "$GO_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: Invalid Go version format: $GO_VERSION"
    echo "Expected format: X.Y.Z (e.g., 1.25.7)"
    exit 1
fi

echo ""
echo "Bumping Go version to $GO_VERSION"
echo "================================================"

echo ""
echo "Updating go.mod..."
if [ -f "$ROOT_DIR/go.mod" ]; then
    sed -i.bak "s/^go [0-9]\+\.[0-9]\+\.[0-9]\+$/go $GO_VERSION/" "$ROOT_DIR/go.mod" && rm -f "$ROOT_DIR/go.mod.bak"
    echo "✓ Updated $ROOT_DIR/go.mod"
else
    echo "✗ go.mod not found"
    exit 1
fi

echo ""
echo "Updating e2etests/go.mod..."
if [ -f "$ROOT_DIR/e2etests/go.mod" ]; then
    sed -i.bak "s/^go [0-9]\+\.[0-9]\+\.[0-9]\+$/go $GO_VERSION/" "$ROOT_DIR/e2etests/go.mod" && rm -f "$ROOT_DIR/e2etests/go.mod.bak"
    echo "✓ Updated $ROOT_DIR/e2etests/go.mod"
else
    echo "✗ e2etests/go.mod not found"
fi

echo ""
echo "Updating Dockerfile..."
if [ -f "$ROOT_DIR/Dockerfile" ]; then
    sed -i.bak "s/FROM golang:[0-9]\+\.[0-9]\+\.[0-9]\+/FROM golang:$GO_VERSION/" "$ROOT_DIR/Dockerfile" && rm -f "$ROOT_DIR/Dockerfile.bak"
    echo "✓ Updated $ROOT_DIR/Dockerfile"
else
    echo "✗ Dockerfile not found"
fi

echo ""
echo "Running go mod tidy..."
echo "-----------------------------------"

pushd "$ROOT_DIR" > /dev/null
echo "Processing root directory..."
go mod tidy
echo "✓ Completed root go mod tidy"
popd > /dev/null

if [ -d "$ROOT_DIR/e2etests" ]; then
    pushd "$ROOT_DIR/e2etests" > /dev/null
    echo "Processing e2etests directory..."
    go mod tidy
    echo "✓ Completed e2etests go mod tidy"
    popd > /dev/null
fi

