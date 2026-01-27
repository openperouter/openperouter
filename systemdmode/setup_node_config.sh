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
    log_error "Optional: Set NODE_CONFIG_DIR env variable to copy files to each node"
    log_error "Example: NODE_CONFIG_DIR=/path/to/config/files $0 pe-kind"
    exit 1
fi

CLUSTER_NAME="$1"

if [[ -n "${NODE_CONFIG_DIR:-}" ]]; then
    if [[ ! -d "$NODE_CONFIG_DIR" ]]; then
        log_error "Config directory does not exist: $NODE_CONFIG_DIR"
        exit 1
    fi
    log_info "Will copy files from $NODE_CONFIG_DIR to each node"
fi

NODES=$(kind get nodes --name "$CLUSTER_NAME" 2>/dev/null)
if [[ -z "$NODES" ]]; then
    log_error "No nodes found for kind cluster: $CLUSTER_NAME"
    log_error "Please check that the cluster exists with: kind get clusters"
    exit 1
fi

NODE_INDEX=0
for NODE in $NODES; do
    log_info "Creating configuration file for node $NODE with nodeIndex=$NODE_INDEX..."

    docker exec "$NODE" mkdir -p /var/lib/openperouter/configs

    docker exec "$NODE" bash -c "cat > /var/lib/openperouter/node-config.yaml <<EOF
nodeIndex: $NODE_INDEX
logLevel: debug
EOF"

    log_info "  Configuration file created successfully"

    if [[ -n "${NODE_CONFIG_DIR:-}" ]]; then
        log_info "  Copying files from $NODE_CONFIG_DIR to node $NODE..."
        for file in "$NODE_CONFIG_DIR"/*; do
            if [[ -f "$file" ]]; then
                filename=$(basename "$file")
                # Copy openpe_*.yaml files to configs subdirectory
                if [[ "$filename" == openpe_*.yaml ]]; then
                    docker cp "$file" "$NODE:/var/lib/openperouter/configs/$filename"
                    log_info "    Copied router config: $filename -> configs/"
                fi
            fi
        done
    fi

    NODE_INDEX=$((NODE_INDEX + 1))
done

log_info "All node configurations created"
