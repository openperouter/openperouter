#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
#
# Collect retis traces for shared NIC mode eBPF debugging.
# Traces TC classify events and skb drops on the underlay interfaces
# (toswitch, ul-host) across all Kind nodes.
#
# Usage:
#   ./hack/retis-collect.sh              # collect until Ctrl-C
#   ./hack/retis-collect.sh --timeout 30 # collect for 30 seconds
#
# Then inspect with:
#   ./hack/retis-inspect.sh
#
# Requires: retis container image (quay.io/retis/retis)

set -euo pipefail

RETIS_IMAGE="${RETIS_IMAGE:-quay.io/retis/retis:stable}"
RETIS_DATA="${RETIS_DATA:-/tmp/retis-shared-nic}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"

mkdir -p "${RETIS_DATA}"

echo "Collecting retis traces for shared NIC eBPF debugging..."
echo "Output: ${RETIS_DATA}"
echo "Press Ctrl-C to stop collection."
echo ""

${CONTAINER_ENGINE} run --rm -it \
    --cap-drop ALL \
    --cap-add SYS_ADMIN \
    --cap-add BPF \
    --cap-add PERFMON \
    --cap-add SYSLOG \
    --cap-add NET_ADMIN \
    --cap-add SYS_PTRACE \
    --security-opt no-new-privileges \
    --pid=host \
    -v /sys/kernel/tracing:/sys/kernel/tracing:ro \
    -v /sys/kernel/debug:/sys/kernel/debug:ro \
    -v /proc:/proc:ro \
    -v "${RETIS_DATA}":/data:rw \
    "${RETIS_IMAGE}" \
    collect \
        --probe tc:tc_classify \
        --probe skb-drop:kfree_skb \
        --filter 'dev =~ toswitch or dev =~ ul-host' \
        --output /data/retis.data \
        "$@"
