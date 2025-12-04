#!/usr/bin/env bash
# Build custom kind node image with OpenVSwitch
# This script builds a single-arch image for local testing and development

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Configuration - can be overridden via environment variables
KIND_NODE_VERSION="${KIND_NODE_VERSION:-v1.31.4}"
IMG_REPO="${IMG_REPO:-quay.io/openperouter}"
IMG_NAME="${IMG_NAME:-kind-node-ovs}"
IMG_TAG="${IMG_TAG:-${KIND_NODE_VERSION}}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"

# Full image reference
IMG="${IMG_REPO}/${IMG_NAME}:${IMG_TAG}"

echo "=========================================="
echo "Building custom kind node image"
echo "=========================================="
echo "  Base: kindest/node:${KIND_NODE_VERSION}"
echo "  Target: ${IMG}"
echo "  Engine: ${CONTAINER_ENGINE}"
echo "=========================================="
echo

cd "${SCRIPT_DIR}"

# Build the image
${CONTAINER_ENGINE} build \
    --build-arg KIND_NODE_VERSION="${KIND_NODE_VERSION}" \
    -t "${IMG}" \
    -f Dockerfile \
    .

echo
echo "=========================================="
echo "Build successful!"
echo "=========================================="
echo "  Image: ${IMG}"
echo
echo "To use this image with kind:"
echo "  export NODE_IMAGE=${IMG}"
echo "  make deploy"
echo
echo "To test the image:"
echo "  docker run --rm --privileged ${IMG} ovs-vsctl show"
echo "=========================================="
