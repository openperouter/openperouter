#!/bin/bash
# Integration test entrypoint for containerized execution.
# This script is used when running make tests for those tests which require ovs
# and namespace creation / deletion.

set -e

echo "Setting up OVS directories..."
mkdir -p /var/run/openvswitch /var/log/openvswitch /etc/openvswitch

echo "Starting OVS services..."

echo "Starting ovsdb-server..."
ovsdb-server --remote=punix:/var/run/openvswitch/db.sock --detach --pidfile

echo "Waiting for ovsdb-server socket..."
timeout=30
count=0
while [ ! -S /var/run/openvswitch/db.sock ]; do
    sleep 0.1
    count=$((count + 1))
    if [ $count -ge $((timeout * 10)) ]; then
        echo "ERROR: ovsdb-server socket not available after ${timeout}s"
        exit 1
    fi
done

echo "Initializing OVS database..."
ovs-vsctl --no-wait init

echo "Starting ovs-vswitchd..."
ovs-vswitchd --detach --pidfile

echo "Waiting for ovs-vswitchd to be ready..."
timeout=30
count=0
until ovs-vsctl show > /dev/null 2>&1; do
    sleep 0.1
    count=$((count + 1))
    if [ $count -ge $((timeout * 10)) ]; then
        echo "ERROR: ovs-vswitchd not ready after ${timeout}s"
        exit 1
    fi
done

echo "OVS is ready"
ovs-vsctl show

umask 0
exec /src/bin/hostnetwork.test -test.v
