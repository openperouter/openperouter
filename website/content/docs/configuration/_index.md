---
weight: 40
title: "Configuration"
description: "How to configure OpenPERouter"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

OpenPERouter requires two main configuration components: the **Underlay** configuration for external router connectivity and a VPN specific configuration for overlays.

All Custom Resources (CRs) must be created in the same namespace where OpenPERouter is deployed (typically `openperouter-system`).

## Underlay Configuration

The underlay configuration establishes BGP sessions with external routers (typically Top-of-Rack switches).

### Basic Underlay Configuration

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
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
| `nics` | array | List of network interface names to move to router namespace | Yes |
| `neighbors` | array | List of BGP neighbors to peer with | Yes |

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
