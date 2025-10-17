#!/bin/bash
# Assign IP addresses to containers
set -euo pipefail

source "$(dirname $(readlink -f $0))/../common.sh"

# Get cluster names from arguments
CLUSTER_NAMES=("$@")

if [[ ${#CLUSTER_NAMES[@]} -eq 0 ]]; then
    echo "Usage: $0 <cluster_name> [cluster_name2] ..."
    echo "Example: $0 pe-kind"
    echo "Example: IP_MAP_FILE=multicluster/ip_map.txt $0 pe-kind-a pe-kind-b"
    exit 1
fi

# Default to single cluster IP map file
IP_MAP_FILE=${IP_MAP_FILE:-"singlecluster/ip_map.txt"}

assign_ips() {
    echo "Assigning IP addresses to containers for clusters: ${CLUSTER_NAMES[*]}"
    echo "Using IP mapping file: ${IP_MAP_FILE}"

    pushd "$(dirname $(readlink -f $0))/.."

    if [[ ! -f "$IP_MAP_FILE" ]]; then
        echo "Error: IP mapping file not found: $IP_MAP_FILE"
        exit 1
    fi

    # Run IP assignment tool
    sudo $(which go) run tools/assign_ips/assign_ips.go -file ${IP_MAP_FILE} -engine ${CONTAINER_ENGINE}

    popd
}

assign_ips