#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Downloads a Fedora Cloud base image and resizes it.
# The resulting qcow2 is mounted into the QEMU containerlab node.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VM_IMAGE="${SCRIPT_DIR}/fedora-cloud.qcow2"

FEDORA_VERSION="${FEDORA_VERSION:-44}"
FEDORA_RELEASE="${FEDORA_RELEASE:-1.7}"
FEDORA_ARCH="${FEDORA_ARCH:-x86_64}"
FEDORA_IMAGE_NAME="Fedora-Cloud-Base-Generic-${FEDORA_VERSION}-${FEDORA_RELEASE}.${FEDORA_ARCH}.qcow2"
FEDORA_IMAGE_URL="https://download.fedoraproject.org/pub/fedora/linux/releases/${FEDORA_VERSION}/Cloud/${FEDORA_ARCH}/images/${FEDORA_IMAGE_NAME}"
FEDORA_ARCHIVE_URL="https://archives.fedoraproject.org/pub/archive/fedora/linux/releases/${FEDORA_VERSION}/Cloud/${FEDORA_ARCH}/images/${FEDORA_IMAGE_NAME}"

mkdir -p "${SCRIPT_DIR}"
# qemu-img resize needs an exclusive write lock, so skip when the image
# already exists (a running VM may hold it).
if [[ -f "${VM_IMAGE}" ]]; then
    echo "Base image already exists at ${VM_IMAGE}, skipping download and resize."
    exit 0
fi

echo "Downloading Fedora Cloud ${FEDORA_VERSION} base image... (${FEDORA_IMAGE_URL})"
temporary_image="${VM_IMAGE}.tmp"
trap 'rm -f "${temporary_image}"' EXIT
if ! curl -fSL -o "${temporary_image}" "${FEDORA_IMAGE_URL}"; then
    echo "Primary URL failed, trying archive mirror... (${FEDORA_ARCHIVE_URL})"
    curl -fSL -o "${temporary_image}" "${FEDORA_ARCHIVE_URL}"
fi
mv "${temporary_image}" "${VM_IMAGE}"
echo "Base image saved to ${VM_IMAGE}"
echo "Resizing VM image to 20G..."
qemu-img resize "${VM_IMAGE}" 20G
