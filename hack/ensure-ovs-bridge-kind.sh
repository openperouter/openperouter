#!/usr/bin/env bash
# Ensure OVS bridge exists in kind nodes
# This script creates an OVS bridge named 'br1' in all nodes of a kind cluster

set -euo pipefail

KIND_BIN="${KIND_BIN:-kind}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-pe-kind}"
CONTAINER_ENGINE="${CONTAINER_ENGINE:-docker}"
BRIDGE_NAME="${BRIDGE_NAME:-br1}"

echo "Ensuring OVS bridge '${BRIDGE_NAME}' exists in kind cluster: ${KIND_CLUSTER_NAME}"

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

# Ensure bridge exists on each node
for NODE in $NODES; do
    echo ""
    echo "=========================================="
    echo "Checking OVS bridge on node: $NODE"
    echo "=========================================="

    # Check if OVS is running
    if ! ${CONTAINER_ENGINE} exec "$NODE" pgrep ovsdb-server > /dev/null; then
        echo "Error: OVS is not running on $NODE"
        echo "Please run './hack/install-ovs-kind.sh' first to install OVS"
        exit 1
    fi

    # Check if bridge already exists
    if ${CONTAINER_ENGINE} exec "$NODE" ovs-vsctl br-exists "${BRIDGE_NAME}" 2>/dev/null; then
        echo "✓ Bridge '${BRIDGE_NAME}' already exists on $NODE"
    else
        echo "Creating bridge '${BRIDGE_NAME}' on $NODE..."
        ${CONTAINER_ENGINE} exec "$NODE" ovs-vsctl add-br "${BRIDGE_NAME}"
        echo "✓ Bridge '${BRIDGE_NAME}' created on $NODE"
    fi

    ${CONTAINER_ENGINE} exec "$NODE" ovs-vsctl set bridge "${BRIDGE_NAME}" stp_enable=true
    ${CONTAINER_ENGINE} exec "$NODE" ip link set "${BRIDGE_NAME}" up
    echo "✓ Bridge '${BRIDGE_NAME}' configured on $NODE"

    # Verify and show bridge configuration
    echo "Bridge configuration:"
    ${CONTAINER_ENGINE} exec "$NODE" ovs-vsctl show | grep -A 5 "Bridge ${BRIDGE_NAME}" || echo "    Bridge ${BRIDGE_NAME}"
done

echo ""
echo "=========================================="
echo "OVS bridge setup complete!"
echo "=========================================="
echo ""
echo "Bridge '${BRIDGE_NAME}' is available on all nodes."
echo ""
echo "Verification:"
echo "  To check the bridge on a node, run:"
echo "  ${CONTAINER_ENGINE} exec <node-name> ovs-vsctl show"
echo ""
echo "  Example:"
echo "  ${CONTAINER_ENGINE} exec ${KIND_CLUSTER_NAME}-control-plane ovs-vsctl show"
echo ""
echo "  To list all bridges:"
echo "  ${CONTAINER_ENGINE} exec ${KIND_CLUSTER_NAME}-control-plane ovs-vsctl list-br"
