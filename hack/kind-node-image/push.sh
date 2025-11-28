#!/usr/bin/env bash
# Push custom kind node image with multi-arch support
# This script builds and pushes images for multiple platforms to the container registry

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Configuration - can be overridden via environment variables
KIND_NODE_VERSION="${KIND_NODE_VERSION:-v1.31.4}"
IMG_REPO="${IMG_REPO:-quay.io/openperouter}"
IMG_NAME="${IMG_NAME:-kind-node-ovs}"
IMG_TAG="${IMG_TAG:-${KIND_NODE_VERSION}}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"

# Platforms to build (aligned with kind's supported platforms)
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

# Full image reference
IMG="${IMG_REPO}/${IMG_NAME}:${IMG_TAG}"
IMG_LATEST="${IMG_REPO}/${IMG_NAME}:latest"

echo "=========================================="
echo "Building and pushing multi-arch kind node image"
echo "=========================================="
echo "  Platforms: ${PLATFORMS}"
echo "  Target: ${IMG}"
echo "  Latest: ${IMG_LATEST}"
echo "=========================================="
echo

cd "${SCRIPT_DIR}"

# Check if buildx is available
if ! ${CONTAINER_ENGINE} buildx version > /dev/null 2>&1; then
    echo "Error: docker buildx is required for multi-arch builds"
    echo "Please install buildx or use build.sh for single-arch builds"
    exit 1
fi

# Create or use existing buildx builder
BUILDER_NAME="kind-node-ovs-builder"
if ! ${CONTAINER_ENGINE} buildx inspect "${BUILDER_NAME}" > /dev/null 2>&1; then
    echo "Creating buildx builder: ${BUILDER_NAME}"
    ${CONTAINER_ENGINE} buildx create --name "${BUILDER_NAME}" --use
else
    echo "Using existing buildx builder: ${BUILDER_NAME}"
    ${CONTAINER_ENGINE} buildx use "${BUILDER_NAME}"
fi

# Build and push multi-arch image
echo
echo "Building and pushing..."
${CONTAINER_ENGINE} buildx build \
    --platform="${PLATFORMS}" \
    --build-arg KIND_NODE_VERSION="${KIND_NODE_VERSION}" \
    --push \
    -t "${IMG}" \
    -t "${IMG_LATEST}" \
    -f Dockerfile \
    .

echo
echo "=========================================="
echo "Push successful!"
echo "=========================================="
echo "  ${IMG}"
echo "  ${IMG_LATEST}"
echo
echo "Images are now available for:"
echo "  ${PLATFORMS}"
echo "=========================================="
