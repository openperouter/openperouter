#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Imports a container image into k3s running inside the QEMU VM.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck disable=SC1091
source "${SCRIPT_DIR}/qemu-common.sh"

IMAGE_REF="${1:?Usage: load-image.sh <image-ref> [tar-path]}"
TAR_PATH="${2:-}"
CLEANUP_TAR=false

cleanup() {
    if [[ "${CLEANUP_TAR}" == true ]]; then
        rm -f "${TAR_PATH}"
    fi
}
trap cleanup EXIT

if [[ -z "${TAR_PATH}" ]]; then
    TAR_PATH=$(mktemp /tmp/qemu-image-XXXXXX.tar)
    echo "Saving ${IMAGE_REF} to ${TAR_PATH}..."
    docker save -o "${TAR_PATH}" "${IMAGE_REF}"
    CLEANUP_TAR=true
fi

echo "Copying image to VM..."
scp_to_vm "${TAR_PATH}" /tmp/openperouter.tar

echo "Importing image into k3s..."
ssh_vm "sudo k3s ctr images import /tmp/openperouter.tar && sudo rm -f /tmp/openperouter.tar"

echo "Image ${IMAGE_REF} loaded into QEMU VM."
