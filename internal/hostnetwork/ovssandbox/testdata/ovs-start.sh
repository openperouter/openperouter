#!/bin/bash
set -e

# Bring up loopback interface
ip link set lo up

# Create OVS database
ovsdb-tool create /var/run/openvswitch/conf.db /usr/share/openvswitch/vswitch.ovsschema

# Start ovsdb-server
ovsdb-server \
    --detach \
    --no-chdir \
    --pidfile=/var/run/openvswitch/ovsdb-server.pid \
    --log-file=/var/run/openvswitch/ovsdb-server.log \
    --remote=punix:/var/run/openvswitch/db.sock \
    --remote=db:Open_vSwitch,Open_vSwitch,manager_options \
    /var/run/openvswitch/conf.db

# Wait for socket to be ready
i=0
while ! [ -S /var/run/openvswitch/db.sock ]; do
    i=$((i+1))
    if [ "$i" -ge 50 ]; then
        echo "Error: OVSDB socket /var/run/openvswitch/db.sock not found after 5 seconds." >&2
        exit 1
    fi
    sleep 0.1
done

# Initialize database
ovs-vsctl --no-wait init

# Start ovs-vswitchd
ovs-vswitchd \
    --detach \
    --no-chdir \
    --pidfile=/var/run/openvswitch/ovs-vswitchd.pid \
    --log-file=/var/run/openvswitch/ovs-vswitchd.log

# Signal that OVS is ready
echo "OVS_SANDBOX_READY"

# Keep container running
exec sleep infinity

