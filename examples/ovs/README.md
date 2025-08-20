# OVS Bridge Example

This example demonstrates how to use OpenPERouter with OVS (Open vSwitch) bridges instead of Linux bridges for host network connectivity.

## Features

- **OVS Bridge Integration**: Uses `libovsdb` to create and manage OVS bridges
- **Automatic Bridge Creation**: Creates OVS bridges automatically when `autocreate: true`
- **Veth Attachment**: Attaches host veth interfaces to OVS bridges as ports
- **L2 Gateway**: Supports distributed L2 gateway functionality over OVS

## Prerequisites

- Open vSwitch installed and running on the host
- OpenPERouter controller deployed
- Kubernetes cluster with appropriate permissions

## Configuration

The key difference from Linux bridge configuration is the `hostmaster.type` field:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: ovs-example
spec:
  vni: 100
  hostmaster:
    type: "ovs-bridge"           # Use OVS instead of Linux bridge
    name: "br-ovs-100"          # OVS bridge name
    autocreate: true            # Create bridge if it doesn't exist
  l2gatewayip: "192.168.100.1/24"
```

## Usage

1. **Setup**: Run the preparation script
   ```bash
   ./prepare.sh
   ```

2. **Verify OVS Configuration**: Check that the bridge was created
   ```bash
   sudo ovs-vsctl show
   ```

3. **Check L2VNI Status**: Verify the VNI is configured
   ```bash
   kubectl get l2vni -n ovs-example
   ```

## Architecture

When using OVS bridges, the network topology looks like:

```
Host Namespace                 OpenPERouter Namespace
┌─────────────────┐           ┌─────────────────────┐
│  OVS Bridge     │           │     Linux Bridge   │
│  (br-ovs-100)   │           │     (br-pe-100)    │
│       │         │           │          │          │
│   host-veth ────┼───────────┼──── pe-veth        │
│                 │           │          │          │
│                 │           │      VXLan iface    │
└─────────────────┘           └─────────────────────┘
```

The controller:
1. Creates an OVS bridge on the host using `libovsdb`
2. Attaches the host-side veth interface as an OVS port
3. Connects the pe-side veth to the Linux bridge in the router namespace

## Troubleshooting

- **OVS not running**: Ensure `systemctl status openvswitch` shows active
- **Permission issues**: The controller needs access to `/var/run/openvswitch/db.sock`
- **Bridge not created**: Check controller logs for OVS connection errors 