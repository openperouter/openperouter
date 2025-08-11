#!/bin/bash

# OVS Bridge Example Preparation Script
# This script sets up the environment for testing OVS bridge functionality

set -e

echo "Setting up OVS bridge example..."

# Ensure OVS is running
if ! systemctl is-active --quiet openvswitch; then
    echo "Starting Open vSwitch service..."
    sudo systemctl start openvswitch
fi

# Create the example namespace if it doesn't exist
kubectl create namespace ovs-example --dry-run=client -o yaml | kubectl apply -f -

# Apply the OpenPE configuration
kubectl apply -f openpe.yaml -n ovs-example

echo "OVS bridge example setup complete!"
echo "The L2VNI will create an OVS bridge 'br-ovs-100' and attach host veth interfaces to it."
echo ""
echo "To verify the setup:"
echo "  sudo ovs-vsctl show"
echo "  kubectl get l2vni -n ovs-example" 