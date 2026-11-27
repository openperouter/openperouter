---
weight: 12
title: "VLAN-aware VNI dataplane"
description: "How every VNI is realized in the kernel, and what upgrading means"
icon: "article"
date: "2026-11-27T00:00:00+01:00"
lastmod: "2026-11-27T00:00:00+01:00"
toc: true
---

OpenPERouter realizes every L2VNI and L3VNI with the same VLAN-aware device
model inside the router network namespace (`/var/run/netns/perouter`). The
model is internal: **there is nothing to configure**. Your CRs, the host-facing
veths, the gateway addresses, and the connectivity your workloads see are
unchanged.

## Device model

For a VNI with ID `<vni>`:

| Device | Role |
|--------|------|
| `br-vl-<vni>` | Private switching bridge with VLAN filtering on and no default PVID. It switches the VNI's ports and carries no addresses or VRF membership. |
| `vni<vni>` | External (`collect-metadata`) VXLAN device with VNI filtering, enslaved to the bridge. The internal VLAN is mapped to the VNI. |
| `br-pe-<vni>` | VLAN subinterface parented on `br-vl-<vni>`. It keeps the name FRR and downstream code already use, and owns the gateway addresses, the anycast MAC, and the VRF membership. |
| router-side veth | Untagged access port on the VNI's private VLAN. |

The VLAN that ties these devices together is internal to the node. It exists
only on the VNI's own bridge and never appears on the wire.

## What this gives you

- **One model for tagged and untagged VNIs.** Standalone (untagged) L2VNIs,
  tagged trunk attachments, and L3VNIs are all built from the same VLAN-aware
  devices. Tagged and untagged VNIs can share a UDP port, so enabling trunks
  does not require a second VXLAN port.
- **Explicit VLAN isolation.** Every per-VNI bridge enables VLAN filtering and
  disables the default PVID before a port is enslaved, so no port inherits an
  implicit VLAN.
- **The same user-facing behavior.** EVPN routes, anycast gateways, VRF
  membership, and host attachments work as before.

## Requirements

VNI filtering requires **Linux kernel 5.18 or later** for all EVPN VNIs. This
replaces the previous recommendation of 5.18 for full IPv6 EVPN support only.
Deployments that use SRv6 only are unaffected.

## Upgrading

Upgrading is automatic. Roll out the new version with your usual method
(`helm upgrade`, `kustomize`, or the all-in-one manifest); each node converges
on its own when its controller pod restarts.

Per node, the controller:

1. Probes the kernel by creating and deleting a temporary VNI-filtering VXLAN
   device in the router namespace. A successful probe is cached for the
   controller process lifetime.
2. If the probe fails, stops before touching the existing devices. The old
   dataplane keeps forwarding, and the node's
   `RouterNodeConfigurationStatus` lists the Underlay as failed with reason
   `ValidationFailed` and a message naming Linux 5.18. Upgrade the kernel and
   the next reconcile retries; no manual cleanup is needed.
3. If the probe succeeds, deletes the legacy per-VNI VXLAN devices on every
   configured UDP port, because a traditional and an external VNI-filtering
   device cannot share a port. Devices that are not named `vni<n>`, and
   devices on ports not configured for a VNI, are left alone.
4. Creates the VLAN-aware devices described above.

Because the VNI devices are replaced during this reconcile, expect a brief
interruption of overlay traffic for the workloads on a node while its dataplane
is rebuilt. The migration happens once per node.

### Verifying the migration

Check the node status for a failed Underlay:

```shell
kubectl -n openperouter-system get routernodeconfigurationstatus -o yaml
```

The controller logs the unsupported-kernel case:

```shell
kubectl -n openperouter-system logs ds/openperouter-controller | grep "VNI filtering"
```

Inspect the device model in the router namespace:

```shell
sudo nsenter --net=/var/run/netns/perouter ip -d link show
```

`br-vl-<vni>` bridges show `vlan_filtering`, their `vni<vni>` devices show
`vnifilter`, and `br-pe-<vni>` is a VLAN interface.

## See also

- [EVPN concepts]({{< ref "/docs/concepts/evpn" >}})
- [EVPN configuration]({{< ref "/docs/configuration/evpn" >}})
- [Node status]({{< ref "/docs/configuration/node-status" >}})
