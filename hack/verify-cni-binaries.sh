#!/bin/bash
# SPDX-License-Identifier:Apache-2.0
#
# Verify that the expected CNI plugin binaries are present and executable
# inside the controller container image.

set -e

CONTAINER_ENGINE="${1:?Usage: $0 <container-engine> <image>}"
IMAGE="${2:?Usage: $0 <container-engine> <image>}"

CNI_BIN_DIR="/opt/openperouter/cni/bin"
BINARIES="macvlan ipvlan static dhcp"

failed=0
for bin in $BINARIES; do
    path="${CNI_BIN_DIR}/${bin}"
    echo -n "Checking ${path}... "
    # CONTAINER_ENGINE is intentionally unquoted to allow word splitting (e.g. "sudo podman")
    if ! ${CONTAINER_ENGINE} run --rm --entrypoint /bin/sh "${IMAGE}" -c "test -x ${path}"; then
        echo "FAIL: ${path} is missing or not executable"
        failed=1
    else
        echo "OK"
    fi
done

if [ "${failed}" -eq 1 ]; then
    echo "ERROR: one or more CNI binaries are missing"
    exit 1
fi

echo "All CNI binaries verified successfully"
