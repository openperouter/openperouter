#!/bin/bash
set -euo pipefail

log_info() {
    echo "[INFO] $*"
}

log_error() {
    echo "[ERROR] $*"
}

if [[ $# -lt 1 ]]; then
    log_error "Usage: $0 <kind-cluster-name>"
    log_error "Example: $0 pe-kind"
    exit 1
fi

CLUSTER_NAME="$1"

NODES=$(kind get nodes --name "$CLUSTER_NAME" 2>/dev/null)
if [[ -z "$NODES" ]]; then
    log_error "No nodes found for kind cluster: $CLUSTER_NAME"
    log_error "Please check that the cluster exists with: kind get clusters"
    exit 1
fi

NODE_INDEX=0
for NODE in $NODES; do
    log_info "Creating configuration file for node $NODE with node_index=$NODE_INDEX..."

    docker exec "$NODE" mkdir -p /var/lib/openperouter

    docker exec "$NODE" bash -c "cat > /var/lib/openperouter/config.yaml <<EOF
node_index: $NODE_INDEX
EOF"

    log_info "  Configuration file created successfully"
    NODE_INDEX=$((NODE_INDEX + 1))
done

log_info "All node configurations created"
