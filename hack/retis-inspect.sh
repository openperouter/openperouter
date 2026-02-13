#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
#
# Inspect retis traces collected by retis-collect.sh.
#
# Usage:
#   ./hack/retis-inspect.sh              # print sorted events
#   ./hack/retis-inspect.sh pcap         # convert to pcap for Wireshark
#   ./hack/retis-inspect.sh sort --last  # show only last events per packet
#
# Requires: retis container image (quay.io/retis/retis)

set -euo pipefail

RETIS_IMAGE="${RETIS_IMAGE:-quay.io/retis/retis:stable}"
RETIS_DATA="${RETIS_DATA:-/tmp/retis-shared-nic}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"

if [ ! -f "${RETIS_DATA}/retis.data" ]; then
    echo "No retis data found at ${RETIS_DATA}/retis.data"
    echo "Run 'make retis-collect' first."
    exit 1
fi

SUBCMD="${1:-sort}"
shift || true

${CONTAINER_ENGINE} run --rm -it \
    -v "${RETIS_DATA}":/data:rw \
    "${RETIS_IMAGE}" \
    "${SUBCMD}" \
        --input /data/retis.data \
        "$@"
