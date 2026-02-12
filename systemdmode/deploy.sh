#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QUADLET_DIR="/etc/containers/systemd"

source "$SCRIPT_DIR/../clab/common.sh"

log_info() {
    echo "[INFO] $*"
}

log_warn() {
    echo "[WARN] $*"
}

log_error() {
    echo "[ERROR] $*"
}

load_image_to_node() {
    local NODE="$1"
    local IMAGE="$2"
    local TEMP_TAR="/tmp/$(basename $IMAGE | tr '/:' '_')-update.tar"

    log_info "    Loading image $IMAGE..."
    $CONTAINER_ENGINE_CLI save "$IMAGE" -o "$TEMP_TAR" 2>/dev/null || {
        log_warn "Failed to save image $IMAGE"
        return 1
    }

    if [[ -f "$TEMP_TAR" ]]; then
        $CONTAINER_ENGINE_CLI cp "$TEMP_TAR" "$NODE:/var/tmp/image-update.tar"
        $CONTAINER_ENGINE_CLI exec "$NODE" podman load -i /var/tmp/image-update.tar
        $CONTAINER_ENGINE_CLI exec "$NODE" rm /var/tmp/image-update.tar
        rm "$TEMP_TAR"
        log_info "    Image $IMAGE loaded successfully"
        return 0
    fi
    return 1
}

update_and_restart_routerpod() {
    local NODE="$1"
    local ROUTER_IMAGE="quay.io/openperouter/router:main"

    log_info "  Updating routerpod images..."
    load_image_to_node "$NODE" "$ROUTER_IMAGE"

    log_info "  Restarting routerpod services..."
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl restart routerpod-pod.service || log_warn "Failed to restart routerpod-pod.service on $NODE"
}

update_and_restart_controllerpod() {
    local NODE="$1"
    local ROUTER_IMAGE="quay.io/openperouter/router:main"

    log_info "  Updating controllerpod images..."
    load_image_to_node "$NODE" "$ROUTER_IMAGE"

    log_info "  Restarting controllerpod services..."
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl restart controllerpod-pod.service || log_warn "Failed to restart controllerpod-pod.service on $NODE"
}

if [[ $# -lt 1 ]]; then
    log_error "Usage: $0 <kind-cluster-name>"
    log_error "Example: $0 my-cluster"
    exit 1
fi

CLUSTER_NAME="$1"

NODES=$(kind get nodes --name "$CLUSTER_NAME" 2>/dev/null)
if [[ -z "$NODES" ]]; then
    log_error "No nodes found for kind cluster: $CLUSTER_NAME"
    log_error "Please check that the cluster exists with: kind get clusters"
    exit 1
fi

for NODE in $NODES; do
    log_info "Deploying to node: $NODE"

    log_info "  Creating quadlet directory..."
    $CONTAINER_ENGINE_CLI exec "$NODE" mkdir -p "$QUADLET_DIR"

    for quadlet_file in "$SCRIPT_DIR"/quadlets/*.pod "$SCRIPT_DIR"/quadlets/*.container "$SCRIPT_DIR"/quadlets/*.volume; do
        if [[ -f "$quadlet_file" ]]; then
            QUADLET_NAME=$(basename "$quadlet_file")
            log_info "    Copying $QUADLET_NAME"
            $CONTAINER_ENGINE_CLI cp "$quadlet_file" "$NODE:$QUADLET_DIR/$QUADLET_NAME"
        fi
    done

    log_info "  Reloading systemd daemon (triggers quadlet generation)..."
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl daemon-reload

    log_info "  Updating images and (re)starting pods..."
    update_and_restart_routerpod "$NODE"
    update_and_restart_controllerpod "$NODE"

    log_info "  Services enabled automatically via quadlet [Install] sections"

    echo ""
done

# Show status for all nodes
log_info "Deployment complete! Showing service status for all nodes:"
echo ""

for NODE in $NODES; do
    log_info "Status for $NODE:"
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl status routerpod-pod.service --no-pager -l 2>&1 || true
    echo ""
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl status controllerpod-pod.service --no-pager -l 2>&1 || true
    echo ""
    log_info "Running containers on $NODE:"
    $CONTAINER_ENGINE_CLI exec "$NODE" podman ps --format "table {{.Names}}\t{{.Status}}\t{{.Image}}" 2>&1 || true
    echo ""
done

