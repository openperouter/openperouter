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
    tunnelEndpoint:
      cidrs:
      - 100.65.0.0/24
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
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
    tunnelEndpoint:
      cidrs:
      - 100.65.0.0/24
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: eth0
    neighbors:
      - asn: 64512
        address: 192.168.111.1
```

### Deferring Startup

If the controller should wait for external dependencies before starting, place an executable script at `/var/lib/openperouter/can_start.sh`. When present, it runs as an `ExecStartPre` step and the controller will not start until the script exits successfully.

Depending on the CNI, the network configuration might need to be complete before the controller starts. For example, DNS resolution might need to be working, or specific network interfaces might need to be available. The script can poll for these conditions and exit with success (code 0) when ready, or exit with failure (non-zero) to prevent startup.

### Dynamic Reload

Configuration files are watched for changes and dynamically reloaded at runtime. Updating a file triggers a reconciliation cycle without restarting the service.

### Kubernetes Visibility

When a kubeconfig is available, the controller mirrors the static configuration to Kubernetes Custom Resources. Each statically-configured resource (underlay, L3VNI, L2VNI, L3Passthrough, RawFRRConfig) is created as a corresponding CR with a `openperouter.io/static-source` label. This makes the full cluster configuration visible via `kubectl`:

```bash
kubectl get underlays -n openperouter-system -l openperouter.io/static-source=true
kubectl get l3vnis -n openperouter-system -l openperouter.io/static-source=true
```

The mirrored resources also go through webhook validation, ensuring that the static configuration is validated against the same rules as API-managed resources.

The static files remain the source of truth: any external modification to the mirrored CRs is reverted on the next reconciliation cycle, and deleting a mirrored CR causes it to be recreated.

### Merging with API Server Configuration

When a kubeconfig is available (exported by the hostbridge pod), configuration from the Kubernetes API server is merged with the file-based configuration. This allows managing part of the configuration via Kubernetes CRs while keeping the base overlay setup in static files that are applied at boot time.
