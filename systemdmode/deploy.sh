#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
QUADLET_DIR="/etc/containers/systemd"
ROUTER_IMAGE="${ROUTER_IMAGE:-quay.io/openperouter/router:main}"

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

if [[ $# -lt 1 ]]; then
    log_error "Usage: $0 <kind-cluster-name>"
    log_error "Example: $0 my-cluster"
    exit 1
fi

CLUSTER_NAME="$1"

NODES=$(${KIND_COMMAND} get nodes --name "$CLUSTER_NAME" 2>/dev/null)
if [[ -z "$NODES" ]]; then
    log_error "No nodes found for kind cluster: $CLUSTER_NAME"
    log_error "Please check that the cluster exists with: ${KIND_COMMAND} get clusters"
    exit 1
fi

for NODE in $NODES; do
    log_info "Deploying to node: $NODE"

    log_info "  Removing hand-installed quadlets, if any..."
    for name in routerpod.pod controllerpod.pod frr.container reloader.container controller.container frr-sockets.volume; do
        $CONTAINER_ENGINE_CLI exec "$NODE" rm -f "$QUADLET_DIR/$name"
    done

    load_image_to_node "$NODE" "$ROUTER_IMAGE"

    log_info "  Installing the bootstrap unit..."
    $CONTAINER_ENGINE_CLI cp "$SCRIPT_DIR/openperouter-bootstrap.service" \
        "$NODE:/etc/systemd/system/openperouter-bootstrap.service"
    $CONTAINER_ENGINE_CLI exec "$NODE" bash -c "mkdir -p /etc/systemd/system/openperouter-bootstrap.service.d && \
        printf '[Service]\nEnvironment=OPENPEROUTER_IMAGE=%s\n' '$ROUTER_IMAGE' \
        > /etc/systemd/system/openperouter-bootstrap.service.d/image.conf"
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl daemon-reload
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl enable openperouter-bootstrap.service

    # A redeploy is meant to be a reboot. Rebooting kind nodes is not
    # practical, so emulate it: stop the runtime, then rerun bootstrap.
    log_info "  Running bootstrap..."
    $CONTAINER_ENGINE_CLI exec "$NODE" systemctl stop routerpod-pod.service controllerpod-pod.service 2>/dev/null || true
    if ! $CONTAINER_ENGINE_CLI exec "$NODE" systemctl restart openperouter-bootstrap.service; then
        log_error "Bootstrap failed on $NODE"
        $CONTAINER_ENGINE_CLI exec "$NODE" journalctl -u openperouter-bootstrap.service --no-pager -n 50 || true
        exit 1
    fi

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

