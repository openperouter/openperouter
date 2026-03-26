---
weight: 60
title: "Systemd Mode"
description: "Configuring OpenPERouter in systemd mode"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

In systemd mode, OpenPERouter is configured via static files on the host instead of Kubernetes Custom Resources. See the [Systemd Mode installation guide]({{< ref "../installation/systemd-mode.md" >}}) for deployment instructions.

## Node Configuration

Each node requires a mandatory configuration file at `/var/lib/openperouter/node-config.yaml`. This file must contain the `nodeIndex`, a unique integer used for IPAM address allocation from the configured CIDRs:

```yaml
nodeIndex: 0
logLevel: debug

Each `openpe_*.yaml` file contains the `spec` part of the corresponding Kubernetes Custom Resources. A file can contain any combination of `underlay`, `l3vnis`, `l2vnis`, `bgppassthrough`, and `rawfrrconfigs` fields, where each entry follows the same schema as the `spec` section of the equivalent CR (Underlay, L3VNI, L2VNI, L3Passthrough, RawFRRConfig):

```yaml
underlays:
  - asn: 64514
    evpn:
      vtepCIDR: 100.65.0.0/24
    nics:
      - eth0
    neighbors:
      - asn: 64512
        address: 192.168.111.1
l3vnis:
  - vrf: red
    vni: 100
    hostSession:
      asn: 64514
      hostASN: 64515
      localCIDR:
        ipv4: "192.169.10.0/24"
        ipv6: "2001:db8:1::/64"
l2vnis:
  - vrf: storage
    vni: 300
    vxlanport: 4789
    hostmaster:
      type: linux-bridge
      linuxBridge:
        name: br-storage
```

### Splitting Configuration Across Files

Multiple files can coexist in the configs directory. This is useful when nodes share a common base configuration but require different underlays. For example, a shared VNI definition can live in one file while per-node underlay settings go in another:

```yaml
# openpe_vni.yaml - common across nodes
l3vnis:
  - vrf: red
    vni: 100
```

```yaml
# openpe_underlay.yaml - node-specific
underlays:
  - asn: 64514
    evpn:
      vtepCIDR: 100.65.0.0/24
    nics:
      - eth0
    neighbors:
      - asn: 64512
        address: 192.168.111.1
```

### Dynamic Reload

Configuration files are watched for changes and dynamically reloaded at runtime. Updating a file triggers a reconciliation cycle without restarting the service.

### Merging with API Server Configuration

When a kubeconfig is available (exported by the hostbridge pod), configuration from the Kubernetes API server is merged with the file-based configuration. This allows managing part of the configuration via Kubernetes CRs while keeping the base overlay setup in static files that are applied at boot time.
