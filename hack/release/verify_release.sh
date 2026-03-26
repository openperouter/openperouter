#!/bin/bash
# Dry-run verification of the cutrelease process.
# Runs the actual release steps in a temporary worktree and discards everything.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

OPENPE_VERSION="${OPENPE_VERSION:-99.99.99}"
export OPENPE_VERSION

WORKTREE_DIR=""

cleanup() {
    if [ -n "$WORKTREE_DIR" ] && [ -d "$WORKTREE_DIR" ]; then
        echo ""
        echo "Cleaning up worktree..."
        cd "$ROOT_DIR"
        git worktree remove --force "$WORKTREE_DIR" 2>/dev/null || rm -rf "$WORKTREE_DIR"
    fi
}

trap cleanup EXIT

echo "================================================"
echo "Release verification dry-run"
echo "Using version: $OPENPE_VERSION"
echo "================================================"

# Create a temporary worktree
WORKTREE_DIR=$(mktemp -d)
BRANCH_NAME=$(git -C "$ROOT_DIR" rev-parse --abbrev-ref HEAD)

git -C "$ROOT_DIR" worktree add --detach "$WORKTREE_DIR" HEAD

cd "$WORKTREE_DIR"

hack/release/bumpversion.sh

make generate-all-in-one

make helm-docs

make api-docs

make bundle

echo ""
echo "================================================"
echo "SUCCESS: All release steps completed successfully"
echo "================================================"
