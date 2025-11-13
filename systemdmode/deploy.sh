#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SYSTEMD_UNIT_DIR="/etc/systemd/system"

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
    docker save "$IMAGE" -o "$TEMP_TAR" 2>/dev/null || {
        log_warn "Failed to save image $IMAGE"
        return 1
    }

    if [[ -f "$TEMP_TAR" ]]; then
        docker cp "$TEMP_TAR" "$NODE:/var/tmp/image-update.tar"
        docker exec "$NODE" podman load -i /var/tmp/image-update.tar
        docker exec "$NODE" rm /var/tmp/image-update.tar
        rm "$TEMP_TAR"
        log_info "    Image $IMAGE loaded successfully"
        return 0
    fi
    return 1
}

update_and_restart_routerpod() {
    local NODE="$1"
    local ROUTER_IMAGE="quay.io/openperouter/router:main"
    local FRR_IMAGE="quay.io/frrouting/frr:10.2.1"

    log_info "  Updating routerpod images..."
    load_image_to_node "$NODE" "$ROUTER_IMAGE"
    load_image_to_node "$NODE" "$FRR_IMAGE"

    log_info "  Restarting routerpod services..."
    docker exec "$NODE" systemctl restart pod-routerpod.service || log_warn "Failed to restart pod-routerpod.service on $NODE"
}

update_and_restart_controllerpod() {
    local NODE="$1"
    local ROUTER_IMAGE="quay.io/openperouter/router:main"

    log_info "  Updating controllerpod images..."
    load_image_to_node "$NODE" "$ROUTER_IMAGE"

    log_info "  Restarting controllerpod services..."
    docker exec "$NODE" systemctl restart pod-controllerpod.service || log_warn "Failed to restart pod-controllerpod.service on $NODE"
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
            docker cp "$service_file" "$NODE:$SYSTEMD_UNIT_DIR/$SERVICE_NAME"
        fi
    done

    docker exec "$NODE" mkdir -p /etc/perouter/frr
    docker exec "$NODE" mkdir -p /var/lib/hostbridge

    log_info "  Reloading systemd daemon..."
    docker exec "$NODE" systemctl daemon-reload

    if docker exec "$NODE" systemctl is-active --quiet pod-controllerpod.service; then
        log_info "  Detected running pods - updating images and restarting..."
        update_and_restart_routerpod "$NODE"
        update_and_restart_controllerpod "$NODE"
    else
        update_and_restart_routerpod "$NODE"
        update_and_restart_controllerpod "$NODE"
    fi

    docker exec "$NODE" systemctl enable pod-routerpod.service pod-controllerpod.service || log_warn "Failed to enable services on $NODE"

    echo ""
done

# Show status for all nodes
log_info "Deployment complete! Showing service status for all nodes:"
echo ""

for NODE in $NODES; do
    docker exec "$NODE" systemctl status pod-routerpod.service --no-pager -l 2>&1 || true
    docker exec "$NODE" systemctl status pod-controllerpod.service --no-pager -l 2>&1 || true
done

