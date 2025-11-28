#!/usr/bin/env bash
# Install Open vSwitch (OVS) in kind nodes
# This script installs and configures OVS inside kind cluster nodes to enable
# OVS bridge functionality for OpenPERouter
#
# DEPRECATED: This script is no longer needed when using the custom kind-node-ovs image.
# OpenVSwitch is pre-installed in quay.io/openperouter/kind-node-ovs images.
# This script is kept for compatibility with older workflows or manual kind clusters
# using the standard kindest/node base image.
#
# For new deployments, use the custom kind image instead:
#   export NODE_IMAGE=quay.io/openperouter/kind-node-ovs:v1.31.4
#   make deploy

set -euo pipefail

echo "=========================================="
echo "WARNING: This script is DEPRECATED"
echo "=========================================="
echo "OpenVSwitch is now pre-installed in the custom kind-node-ovs image."
echo "Consider using: export NODE_IMAGE=quay.io/openperouter/kind-node-ovs:v1.31.4"
echo "See hack/kind-node-image/README.md for more information."
echo "=========================================="
echo ""
sleep 3

KIND_BIN="${KIND_BIN:-kind}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-pe-kind}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"

echo "Installing Open vSwitch in kind cluster: ${KIND_CLUSTER_NAME}"

# Check if cluster exists
if ! "${KIND_BIN}" get clusters | grep -q "^${KIND_CLUSTER_NAME}$"; then
    echo "Error: Kind cluster '${KIND_CLUSTER_NAME}' not found"
    echo "Available clusters:"
    "${KIND_BIN}" get clusters
    exit 1
fi

# Get all nodes in the cluster
NODES=$("${KIND_BIN}" get nodes --name="${KIND_CLUSTER_NAME}")

if [ -z "$NODES" ]; then
    echo "Error: No nodes found in cluster '${KIND_CLUSTER_NAME}'"
    exit 1
fi

echo "Found nodes: $NODES"

# Install OVS on each node
for NODE in $NODES; do
    echo ""
    echo "=========================================="
    echo "Installing OVS on node: $NODE"
    echo "=========================================="

    # Update package lists
    echo "Updating package lists..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "apt-get update -qq"

    # Install OVS packages
    echo "Installing openvswitch-switch package..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq openvswitch-switch"

    # Create necessary directories
    echo "Setting up OVS directories..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "mkdir -p /var/run/openvswitch /var/log/openvswitch /etc/openvswitch"

    # Initialize OVS database if it doesn't exist
    echo "Initializing OVS database..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "
        if [ ! -f /etc/openvswitch/conf.db ]; then
            ovsdb-tool create /etc/openvswitch/conf.db /usr/share/openvswitch/vswitch.ovsschema
        fi
    "

    # Start ovsdb-server
    echo "Starting ovsdb-server..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "
        if ! pgrep ovsdb-server > /dev/null; then
            ovsdb-server --remote=punix:/var/run/openvswitch/db.sock \
                --remote=db:Open_vSwitch,Open_vSwitch,manager_options \
                --pidfile --detach
        else
            echo 'ovsdb-server is already running'
        fi
    "

    # Wait for socket to be ready
    echo "Waiting for OVS database socket..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "
        for i in {1..30}; do
            if [ -S /var/run/openvswitch/db.sock ]; then
                echo 'OVS database socket is ready'
                break
            fi
            echo 'Waiting for socket...'
            sleep 1
        done
        if [ ! -S /var/run/openvswitch/db.sock ]; then
            echo 'Error: OVS database socket not created'
            exit 1
        fi
    "

    # Initialize OVS
    echo "Initializing OVS..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "
        ovs-vsctl --no-wait init || true
    "

    # Start ovs-vswitchd
    echo "Starting ovs-vswitchd..."
    ${CONTAINER_ENGINE} exec "$NODE" bash -c "
        if ! pgrep ovs-vswitchd > /dev/null; then
            ovs-vswitchd --pidfile --detach
        else
            echo 'ovs-vswitchd is already running'
        fi
    "

    # Verify OVS is working
    echo "Verifying OVS installation..."
    ${CONTAINER_ENGINE} exec "$NODE" ovs-vsctl show

    echo "✓ OVS successfully installed on $NODE"
done

echo ""
echo "=========================================="
echo "OVS installation complete!"
echo "=========================================="
echo ""
echo "Verification:"
echo "  To check OVS status on a node, run:"
echo "  ${CONTAINER_ENGINE} exec <node-name> ovs-vsctl show"
echo ""
echo "  Example:"
echo "  ${CONTAINER_ENGINE} exec ${KIND_CLUSTER_NAME}-control-plane ovs-vsctl show"
