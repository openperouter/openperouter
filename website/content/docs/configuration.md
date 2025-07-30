---
weight: 40
title: "Configuration"
description: "How to configure OpenPERouter"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

OpenPERouter requires two main configuration components: the **Underlay** configuration for external router connectivity and **VNI** configurations for EVPN overlays.

All Custom Resources (CRs) must be created in the same namespace where OpenPERouter is deployed (typically `openperouter-system`).

## Underlay Configuration

The underlay configuration establishes BGP sessions with external routers (typically Top-of-Rack switches) and defines the VTEP IP allocation strategy.

### Basic Underlay Configuration

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  vtepcidr: 100.65.0.0/24
  nics:
    - toswitch
  neighbors:
    - asn: 64512
      address: 192.168.11.2
```

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `asn` | integer | Local ASN for BGP sessions | Yes |
| `vtepcidr` | string | CIDR block for VTEP IP allocation | Yes |
| `nics` | array | List of network interface names to move to router namespace | Yes |
| `neighbors` | array | List of BGP neighbors to peer with | Yes |

### VTEP IP Allocation

The `vtepcidr` field defines the IP range used for VTEP (Virtual Tunnel End Point) addresses. OpenPERouter automatically assigns a unique VTEP IP to each node from this range. For example, with `100.65.0.0/24`:

- Node 1: `100.65.0.1`
- Node 2: `100.65.0.2`
- Node 3: `100.65.0.3`
- etc.

### Alternative: Multus Network for Top of Rack Connectivity

Instead of declaring physical network interfaces in the underlay configuration, you can use Multus networks to provide connectivity to top of rack switches. In this case, the `nics` field in the underlay configuration can be omitted.

When using this approach, ensure that the router pods are configured with the appropriate Multus network annotation to connect to your top of rack switches.

#### Using Helm Values

You can specify the Multus network annotation using Helm values:

```yaml
# values.yaml
openperouter:
  multusNetworkAnnotation: "macvlan-conf"
```

Or when installing with Helm:

```bash
helm install openperouter ./charts/openperouter \
  --set openperouter.multusNetworkAnnotation="macvlan-conf"
```

This will add the annotation `k8s.v1.cni.cncf.io/networks: macvlan-conf` to the router pods.

#### Using Kustomize

Alternatively, you can use kustomize to add the annotation to the router pod:

```yaml
# kustomization.yaml
patches:
- target:
    kind: DaemonSet
    name: router
  patch: |-
    - op: add
      path: /spec/template/metadata/annotations
      value:
        k8s.v1.cni.cncf.io/networks: macvlan-conf
```

## L3 VNI Configuration

L3 VNI (Virtual Network Identifier) configurations define EVPN L3 overlays. Each L3VNI creates a separate routing domain and BGP session with the host.

### Basic L3VNI Configuration

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: blue
  namespace: openperouter-system
spec:
  vni: 200
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.169.11.0/24
```

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `vni` | integer | Virtual Network Identifier (1-16777215) | Yes |
| `hostsession.asn` | integer | Router ASN for BGP session with host | Yes |
| `hostsession.hostasn` | integer | Host ASN for BGP session | Yes |
| `hostsession.localcidr` | object | CIDR for veth pair IP allocation | Yes |

### Multiple VNIs Example

You can create multiple VNIs for different network segments:

```yaml
# Production VNI
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: signal
  namespace: openperouter-system
spec:
  vni: 100
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.168.10.0/24
---
# Development VNI
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L3VNI
metadata:
  name: oam
  namespace: openperouter-system
spec:
  vni: 200
  hostsession:
    asn: 64514
    hostasn: 64515
    localcidr:
      ipv4: 192.168.20.0/24
```

## What Happens During Reconciliation

When you create or update VNI configurations, OpenPERouter automatically:

1. **Creates Network Interfaces**: Sets up VXLAN interface and Linux VRF named after the VNI
2. **Establishes Connectivity**: Creates veth pair and moves one end to the router's namespace
3. **Assigns IP Addresses**: Allocates IPs from the `localcidr` range:
   - Router side: First IP in the CIDR (e.g., `192.169.11.1`)
   - Host side: Each node gets a free IP in the CIDR, starting from the second (e.g., `192.169.11.15`)
4. **Creates BGP Session**: Opens BGP session between router and host using the specified ASNs

## L2VNI Configuration

L2VNIs provide Layer 2 connectivity across nodes using EVPN tunnels. Unlike L3VNIs, L2VNIs extend Layer 2 domains rather than routing domains.

### Configuration Fields

| Field | Type | Description | Required |
|-------|------|-------------|----------|
| `vni` | integer | Virtual Network Identifier for the EVPN tunnel | Yes |
| `vrf` | string | Name of the VRF to associate with this L2VNI | Yes |
| `hostmaster.type` | string | Type of host interface management (`bridge` or `direct`) | Yes |
| `hostmaster.autocreate` | boolean | Whether to automatically create a bridge if type is `bridge` | No |
| `hostmaster.bridgeName` | string | Name of the bridge to attach to (if not auto-creating) | No |

### L2VNI Example

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: l2red
  namespace: openperouter-system
spec:
  vni: 210
  vrf: red
  hostmaster:
    type: bridge
    autocreate: true
```

## What Happens During Reconciliation

When you create or update VNI configurations, OpenPERouter automatically:

1. **Creates Network Interfaces**: Sets up VXLAN interface and Linux VRF named after the VNI
2. **Establishes Connectivity**: Creates veth pair and moves one end to the router's namespace
3. **Enslaves the veth**: the veth is connected to the bridge corresponding to the l2 domain
4. **Optionally creates a bridge on the host**: if hostmaster.autocreate is set to `true`
5. **Optionally connects the host veth to the bridge on the host**: if hostmaster.autocreate is set to `true` or name
is set

## API Reference

For detailed information about all available configuration fields, validation rules, and API specifications, see the [API Reference]({{< ref "api-reference.md" >}}) documentation.
