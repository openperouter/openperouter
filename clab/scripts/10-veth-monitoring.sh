#!/bin/bash
# Setup veth monitoring
set -euo pipefail

source "$(dirname $(readlink -f $0))/../common.sh"

# Get cluster names from arguments
CLUSTER_NAMES=("$@")

if [[ ${#CLUSTER_NAMES[@]} -eq 0 ]]; then
    echo "Usage: $0 <cluster_name> [cluster_name2] ..."
    echo "Example: $0 pe-kind"
    echo "Example: $0 pe-kind-a pe-kind-b"
    exit 1
fi

setup_veth_monitoring() {
    echo "Setting up veth monitoring for clusters: ${CLUSTER_NAMES[*]}"

    pushd "$(dirname $(readlink -f $0))/.."

    # Check if monitoring is already running to avoid duplicates
    if ! pgrep -f check_veths.sh | xargs -r ps -p | grep -q pe-kind; then
        echo "Starting veth monitoring"

        if [[ ${#CLUSTER_NAMES[@]} -eq 1 && "${CLUSTER_NAMES[0]}" == "pe-kind" ]]; then
            # Single cluster mode
            sudo -E ./check_veths.sh \
                kindctrlpl:toswitch:pe-kind-control-plane:192.168.11.3/24 \
                kindworker:toswitch:pe-kind-worker:192.168.11.4/24 &
        else
            # Multi-cluster mode
            sudo -E ./check_veths.sh \
                kindctrlpla:toswitch:pe-kind-a-control-plane:192.168.11.3/24 \
                kindworkera:toswitch:pe-kind-a-worker:192.168.11.4/24 \
                kindctrlplb:toswitch:pe-kind-b-control-plane:192.168.12.3/24 \
                kindworkerb:toswitch:pe-kind-b-worker:192.168.12.4/24 &
        fi
    else
        echo "Veth monitoring already running"
    fi

    # Give some time for the monitoring to start
    sleep 4s

    popd
}

setup_veth_monitoring