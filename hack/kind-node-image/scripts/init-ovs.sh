#!/usr/bin/env bash
# Initialize OVS services at container startup
# This runs early in the container lifecycle to ensure OVS is available

set -euo pipefail

echo "Initializing OpenVSwitch..."

# Ensure directories exist
mkdir -p /var/run/openvswitch /var/log/openvswitch /etc/openvswitch

# Start ovsdb-server if not running
if ! pgrep -x ovsdb-server > /dev/null; then
    echo "Starting ovsdb-server..."
    ovsdb-server --remote=punix:/var/run/openvswitch/db.sock \
        --remote=db:Open_vSwitch,Open_vSwitch,manager_options \
        --pidfile --detach
else
    echo "ovsdb-server is already running"
fi

# Wait for socket to be ready
echo "Waiting for OVS database socket..."
for i in {1..30}; do
    if [ -S /var/run/openvswitch/db.sock ]; then
        echo "OVS database socket is ready"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "Error: OVS database socket not created after 30 seconds"
        exit 1
    fi
    sleep 0.5
done

# Initialize OVS
echo "Initializing OVS database..."
ovs-vsctl --no-wait init || true

# Start ovs-vswitchd if not running
if ! pgrep -x ovs-vswitchd > /dev/null; then
    echo "Starting ovs-vswitchd..."
    ovs-vswitchd --pidfile --detach
else
    echo "ovs-vswitchd is already running"
fi

# Wait a moment for ovs-vswitchd to fully start
sleep 1

# Verify OVS is working
if ovs-vsctl show > /dev/null 2>&1; then
    echo "OpenVSwitch initialized successfully"
else
    echo "Warning: ovs-vsctl show failed, but continuing..."
fi
