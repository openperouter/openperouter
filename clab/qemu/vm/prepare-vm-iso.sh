#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Creates a cloud-init ISO with an injected SSH public key.
# The resulting ISO is mounted into the QEMU containerlab node.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/qemu-common.sh"
CLOUD_INIT_DIR="${SCRIPT_DIR}/cloud-init"
CLOUD_INIT_ISO="${SCRIPT_DIR}/cloud-init.iso"

mkdir -p "${SCRIPT_DIR}"

if [[ ! -f "${SSH_KEY}" ]]; then
    echo "Generating SSH key at ${SSH_KEY}..."
    ssh-keygen -t ed25519 -f "${SSH_KEY}" -N "" -q
fi

if [[ -f "${CLOUD_INIT_ISO}" ]]; then
    echo "cloud-init ISO already exists at ${CLOUD_INIT_ISO}, skipping."
    exit 0
fi

echo "Preparing cloud-init data..."
build_dir=$(mktemp -d)
trap 'rm -rf "${build_dir}"' EXIT

# Copy static meta-data
cp "${CLOUD_INIT_DIR}/meta-data" "${build_dir}/meta-data"

# Inject the generated public key into user-data
pub_key="$(cat "${SSH_KEY}.pub")"
sed "s|ssh_authorized_keys: \[\]|ssh_authorized_keys:\n      - ${pub_key}|g" "${CLOUD_INIT_DIR}/user-data" > "${build_dir}/user-data"

echo "Creating cloud-init ISO..."
if command -v genisoimage &>/dev/null; then
    genisoimage -output "${CLOUD_INIT_ISO}" -volid cidata -joliet -rock \
        "${build_dir}/user-data" "${build_dir}/meta-data"
elif command -v mkisofs &>/dev/null; then
    mkisofs -output "${CLOUD_INIT_ISO}" -volid cidata -joliet -rock \
        "${build_dir}/user-data" "${build_dir}/meta-data"
elif command -v xorrisofs &>/dev/null; then
    xorrisofs -output "${CLOUD_INIT_ISO}" -volid cidata -joliet -rock \
        "${build_dir}/user-data" "${build_dir}/meta-data"
else
    echo "ERROR: No ISO creation tool found (genisoimage, mkisofs, or xorrisofs)." >&2
    exit 1
fi

echo "cloud-init ISO created at ${CLOUD_INIT_ISO}"
