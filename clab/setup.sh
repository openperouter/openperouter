#!/bin/bash
set -euo pipefail
set -x

pushd "$(dirname $(readlink -f $0))"
source common.sh

CALICO_MODE=${CALICO_MODE:-false}
CLAB_TOPOLOGY="${CLAB_TOPOLOGY:-singlecluster/kind.clab.yml}"
IP_MAP_FILE=${IP_MAP_FILE:-"singlecluster/ip_map.txt"}

# Get cluster names from command line arguments or environment variable, default to single cluster
if [[ $# -gt 0 ]]; then
    # Use command line arguments as cluster names
    CLUSTER_ARRAY=("$@")
elif [[ -n "${CLUSTER_NAMES:-}" ]]; then
    # Convert space-separated string to array
    read -ra CLUSTER_ARRAY <<< "$CLUSTER_NAMES"
else
    # Default to single cluster
    CLUSTER_ARRAY=("pe-kind")
fi

# Check if clusters already exist
for cluster_name in "${CLUSTER_ARRAY[@]}"; do
    clusters=$(${KIND_COMMAND} get clusters)
    for cluster in $clusters; do
        if [[ $cluster == "$cluster_name" ]]; then
            echo "Cluster ${cluster_name} already exists"
            exit 0
        fi
    done
done

# Determine mode for logging
if [[ ${#CLUSTER_ARRAY[@]} -gt 1 ]]; then
    CLUSTER_MODE="multi"
    export IP_MAP_FILE="multicluster/ip_map.txt"
else
    CLUSTER_MODE="single"
fi

echo "=== Starting ${CLUSTER_MODE} cluster deployment ==="
echo "CALICO_MODE: $CALICO_MODE"
echo "CLAB_TOPOLOGY: $CLAB_TOPOLOGY"
echo "CONTAINER_ENGINE: $CONTAINER_ENGINE"
echo "CLUSTER_NAMES: ${CLUSTER_ARRAY[*]}"

# Execute setup scripts in sequence using modular scripts
echo "=== 1/11 Environment setup ==="
./scripts/00-environment.sh "${CLUSTER_ARRAY[@]}"

echo "=== 2/11 Registry setup ==="
./scripts/01-registry.sh "${CLUSTER_ARRAY[@]}"

echo "=== 3/11 Leaf configurations ==="
./scripts/02-leaf-configs.sh

echo "=== 4/11 Kind configurations ==="
./scripts/03-kind-configs.sh "${CLUSTER_ARRAY[@]}"

echo "=== 5/11 Containerlab deployment ==="
./scripts/04-containerlab-deploy.sh

echo "=== 6/11 Loading images ==="
./scripts/05-load-images.sh "${CLUSTER_ARRAY[@]}"

echo "=== 7/11 Kubeconfig setup ==="
./scripts/06-kubeconfig-setup.sh "${CLUSTER_ARRAY[@]}"

echo "=== 8/11 FRR-k8s setup ==="
./scripts/07-frr-k8s-setup.sh "${CLUSTER_ARRAY[@]}"

echo "=== 9/11 IP assignment ==="
./scripts/08-ip-assignment.sh "${CLUSTER_ARRAY[@]}"

echo "=== 10/11 Container setup ==="
./scripts/09-container-setup.sh "${CLUSTER_ARRAY[@]}"

echo "=== 11/11 Veth monitoring ==="
./scripts/10-veth-monitoring.sh "${CLUSTER_ARRAY[@]}"

echo "=== ${CLUSTER_MODE^} cluster deployment completed ==="
if [[ "$CLUSTER_MODE" == "single" ]]; then
    echo "Cluster created: ${CLUSTER_ARRAY[0]} (kubeconfig: ${KUBECONFIG_PATH})"
else
    echo "Clusters created:"
    for cluster_name in "${CLUSTER_ARRAY[@]}"; do
        echo "  - ${cluster_name} (kubeconfig: ${KUBECONFIG_PATH}-${cluster_name})"
    done
    echo "Default kubeconfig: ${KUBECONFIG_PATH} (points to cluster ${CLUSTER_ARRAY[0]})"
fi

popd
