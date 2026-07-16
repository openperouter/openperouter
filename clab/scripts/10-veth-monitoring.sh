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
    pid=$(cat "${PID_FILE}" 2>/dev/null || true)
    if ps -q "${pid}" >/dev/null 2>&1; then
        echo "Found process ID ${pid} for check_veths, process is already running"
        return
    fi

    echo "Rebuilding check_veth tool"

    pushd tools/check_veths/
    "$(which go)" build -o bin/check_veths .
    popd

    echo "Starting veth monitoring"

    CHECK_VETHS_LOG="/tmp/check_veths.log"
    if [[ ${#CLUSTER_NAMES[@]} -eq 1 && "${CLUSTER_NAMES[0]}" == "pe-kind" ]]; then
        sudo tools/check_veths/bin/check_veths -f singlecluster/check-veths.yaml -p "${PID_FILE}" \
          > "$CHECK_VETHS_LOG" 2>&1 &
    else
        sudo tools/check_veths/bin/check_veths -f multicluster/check-veths.yaml -p "${PID_FILE}" \
          > "$CHECK_VETHS_LOG" 2>&1 &
    fi

    popd
}

setup_veth_monitoring
