#!/bin/bash

set -e


K8S_VERSION=${1:-}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -z "$K8S_VERSION" ]; then
    echo "No version specified, fetching latest k8s.io version..."
    echo ""

    # Fetch latest version from k8s.io/api
    K8S_VERSION=$(go list -m -versions k8s.io/api 2>/dev/null | tr ' ' '\n' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)

    if [ -z "$K8S_VERSION" ]; then
        echo "Error: Failed to fetch latest k8s.io version"
        echo "Please specify a version manually:"
        echo "Usage: $0 <k8s-version>"
        echo "Example: $0 v0.34.0"
        exit 1
    fi

    echo "Latest k8s.io version found: $K8S_VERSION"
fi

echo ""
echo "Bumping Kubernetes dependencies to $K8S_VERSION"
echo "================================================"

bump_deps() {
    local dir=$1
    local dir_name=$(basename "$dir")

    if [ "$dir" = "$ROOT_DIR" ]; then
        dir_name="root"
    fi

    echo ""
    echo "Processing $dir_name directory..."
    echo "-----------------------------------"

    pushd "$dir" > /dev/null

    echo "Finding k8s.io dependencies..."
    K8S_DEPS=$(go list -m -f '{{.Path}}' all | grep '^k8s.io/' | sort -u || true)

    echo "Finding sigs.k8s.io dependencies..."
    SIGS_DEPS=$(go list -m -f '{{.Path}}' all | grep '^sigs.k8s.io/' | sort -u || true)

    if [ -n "$K8S_DEPS" ]; then
        echo ""
        echo "Updating k8s.io dependencies to $K8S_VERSION:"
        for dep in $K8S_DEPS; do
            echo "  - $dep"
            go get "$dep@$K8S_VERSION" || {
                echo "    WARNING: Failed to update $dep to $K8S_VERSION, trying latest..."
                go get "$dep@latest" || echo "    ERROR: Failed to update $dep"
            }
        done
    fi

    # Bump sigs.k8s.io dependencies to latest compatible version
    if [ -n "$SIGS_DEPS" ]; then
        echo ""
        echo "Updating sigs.k8s.io dependencies to latest compatible versions:"
        for dep in $SIGS_DEPS; do
            echo "  - $dep"
            go get "$dep@latest" || echo "    ERROR: Failed to update $dep"
        done
    fi

    # Run go mod tidy
    echo ""
    echo "Running go mod tidy..."
    go mod tidy

    popd > /dev/null

    echo "✓ Completed $dir_name"
}

bump_deps "$ROOT_DIR"

bump_deps "$ROOT_DIR/e2etests"

