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
    if pgrep -f check_veths.go | xargs -r ps -p | grep -q pe-kind; then
        echo "Veth monitoring already running"
    else
        echo "Starting veth monitoring"

        CHECK_VETHS_LOG="/tmp/check_veths.log"
        if [[ ${#CLUSTER_NAMES[@]} -eq 1 && "${CLUSTER_NAMES[0]}" == "pe-kind" ]]; then
            # Single cluster mode
            sudo -E "$(which go)" run tools/check_veths/*.go -f singlecluster/check-veths.yaml  2>&1 | \
              awk '{print strftime("%Y-%m-%dT%H:%M:%S"), $0; fflush()}' > "$CHECK_VETHS_LOG" &
        else
            # Multi-cluster mode
            sudo -E "$(which go)" run tools/check_veths/*.go -f multicluster/check-veths.yaml 2>&1 | \
              awk '{print strftime("%Y-%m-%dT%H:%M:%S"), $0; fflush()}' > "$CHECK_VETHS_LOG" &
        fi
    fi

    # Give some time for the monitoring to start
    sleep 4s

    popd
}

setup_veth_monitoring
