#!/usr/bin/env bash
# Build custom kind node image with OpenVSwitch
# This script builds a single-arch image for local testing and development

set -euo pipefail

# Configuration - can be overridden via environment variables
KIND_NODE_VERSION="${KIND_NODE_VERSION:-v1.32.11}"
IMG_REPO="${IMG_REPO:-quay.io/openperouter}"
IMG_NAME="${IMG_NAME:-kind-node-openperouter}"
IMG_TAG="${IMG_TAG:-${KIND_NODE_VERSION}}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"
IMG="${IMG_REPO}/${IMG_NAME}:${IMG_TAG}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${SCRIPT_DIR}"

# Build the image
${CONTAINER_ENGINE} build \
    --build-arg KIND_NODE_VERSION="${KIND_NODE_VERSION}" \
    -t "${IMG}" \
    -f Dockerfile \
    .
