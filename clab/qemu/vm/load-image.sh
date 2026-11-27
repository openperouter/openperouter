#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Imports a container image into k3s on both QEMU VMs.

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
    CLEANUP_TAR=true
    docker save -o "${TAR_PATH}" "${IMAGE_REF}"
fi

for port in "${QEMU_SSH_PORT}" "${QEMU_WORKER_SSH_PORT}"; do
    echo "Importing ${IMAGE_REF} on the VM at SSH port ${port}..."
    QEMU_SSH_PORT="${port}" scp_to_vm "${TAR_PATH}" /tmp/openperouter.tar
    QEMU_SSH_PORT="${port}" ssh_vm "sudo k3s ctr images import /tmp/openperouter.tar && sudo rm -f /tmp/openperouter.tar"
done
