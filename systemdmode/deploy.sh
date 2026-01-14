#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSTEMD_UNIT_DIR="/etc/systemd/system"

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
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl restart pod-routerpod.service || log_warn "Failed to restart pod-routerpod.service on $NODE"
}

update_and_restart_controllerpod() {
    local NODE="$1"
    local ROUTER_IMAGE="quay.io/openperouter/router:main"

    log_info "  Updating controllerpod images..."
    load_image_to_node "$NODE" "$ROUTER_IMAGE"

    log_info "  Restarting controllerpod services..."
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl restart pod-controllerpod.service || log_warn "Failed to restart pod-controllerpod.service on $NODE"
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

    for service_file in "$SCRIPT_DIR"/pod-*.service "$SCRIPT_DIR"/container-*.service; do
        if [[ -f "$service_file" ]]; then
            SERVICE_NAME=$(basename "$service_file")
            log_info "    Copying $SERVICE_NAME"
            $CONTAINER_ENGINE_CLI cp "$service_file" "$NODE:$SYSTEMD_UNIT_DIR/$SERVICE_NAME"
        fi
    done

    $CONTAINER_ENGINE_CLI exec "$NODE" mkdir -p /etc/perouter/frr
    $CONTAINER_ENGINE_CLI exec "$NODE" mkdir -p /var/lib/hostbridge

    log_info "  Reloading systemd daemon..."
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl daemon-reload

    if $CONTAINER_ENGINE_CLI exec "$NODE" systemctl is-active --quiet pod-controllerpod.service; then
        log_info "  Detected running pods - updating images and restarting..."
        update_and_restart_routerpod "$NODE"
        update_and_restart_controllerpod "$NODE"
    else
        update_and_restart_routerpod "$NODE"
        update_and_restart_controllerpod "$NODE"
    fi

    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl enable pod-routerpod.service pod-controllerpod.service || log_warn "Failed to enable services on $NODE"

    echo ""
done

# Show status for all nodes
log_info "Deployment complete! Showing service status for all nodes:"
echo ""

for NODE in $NODES; do
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl status pod-routerpod.service --no-pager -l 2>&1 || true
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl status pod-controllerpod.service --no-pager -l 2>&1 || true
done

