---
weight: 44
title: "Shared VLAN Trunks"
description: "Carry many tagged L2VNIs over one host attachment with an L2Trunk"
icon: "article"
date: "2026-11-18T10:00:00+02:00"
lastmod: "2026-11-18T10:00:00+02:00"
toc: true
---

An `L2Trunk` carries several tagged L2VNIs over a single host attachment. It
exists for the shape where a host has one port connected to a VLAN-aware fabric
and many tenants share it, as opposed to one host interface per L2VNI.

Each L2VNI that references a trunk keeps its own VNI, gateway, routing domain
and `br-pe-<vni>` endpoint: the trunk only decides the port and the tag its
traffic uses on the way to the host.

## Configuration

An `L2Trunk` selects the host-side port the trunk attaches to:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2Trunk
metadata:
  name: workloads
  namespace: openperouter-system
spec:
  type: Host
  hostMaster:
    type: LinuxBridge
    linuxBridge:
      lifecycle: External
      name: br-trunk
```

`hostMaster.linuxBridge.lifecycle` must be `External`: the bridge is owned by
the platform, not by OpenPERouter. Only `type: Host` is defined today. When
`hostMaster` is omitted the host leg is created but left unattached, exactly
like an L2VNI without a host master.

An L2VNI joins the trunk with `spec.trunk`, which names the trunk and the VLAN
tag it uses:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-a
  namespace: openperouter-system
spec:
  vni: 110
  routingDomain:
    type: L3VNI
    l3vni:
      name: red
  trunk:
    name: workloads
    vlan:
      tag: 100
  gatewayIPs: ["192.170.1.1/24"]
```

`spec.trunk` and `spec.hostMaster` are mutually exclusive. The trunk reference
and tag are immutable: moving an L2VNI between tags or between the standalone
and trunk models is done by recreating the L2VNI.

Every L2VNI of a trunk must agree on `vxlanPort` and `underlayAddressFamily`,
because they share one VXLAN device. Gateway, routing domain, import and export
route targets stay per L2VNI. The tag-to-tenant mapping is unique inside a
trunk, and also across trunks that share the same external host master.

The existing [EVPN configuration]({{< ref "evpn.md" >}}) and
[routing domain]({{< ref "evpn.md" >}}) rules apply to the L2VNI
itself; the trunk does not change them.

## Requirements

- A Linux kernel with VXLAN VNI filtering support (5.18 or newer). OpenPERouter
  probes the capability at startup and refuses to tear down an existing
  dataplane when it is missing.
- A VLAN-aware host bridge (`vlan_filtering 1`). Prefer
  `vlan_default_pvid 0` so untagged frames are not leaked into a tenant domain.
- The bridge ports that fan the tags out to workloads are the operator's
  responsibility, as described below.

## Device naming and lookup

Device names hash the trunk's namespace and name, so two trunks with the same
name in different namespaces never share a bridge:

| Device | Netns | Purpose |
|--------|-------|---------|
| `host-t-<id>` | host | trunk host leg, attached to `hostMaster` |
| `pe-t-<id>` | router | router-side leg, member of the trunk bridge |
| `br-t-<id>` | router | shared VLAN-aware bridge |
| `vxt-<id>` | router | shared external (vnifilter) VXLAN device |

`<id>` is the first four bytes of the SHA-256 of `<namespace>/<name>`, hex
encoded. The same identifier is written to the resource as the
`network.openperouter.io/trunk-id` label, and every device carries the
`<namespace>/<name>` alias:

```bash
kubectl get l2trunks.network.openperouter.io -n openperouter-system \
  -o custom-columns=NAME:.metadata.name,ID:.metadata.labels.network\\.openperouter\\.io/trunk-id
ip -d link show host-t-<id>   # alias shows the resource that owns it
```

The per-L2VNI endpoint keeps the name it has without a trunk:
`br-pe-<vni>`. It stays the interface that owns the gateway addresses and the
neighbour table.

## Trust boundary

The trunk host leg is trusted to carry any tag that is configured on it.
OpenPERouter maps frame tags to VNIs; it does not authenticate the source of a
frame. A workload attached to a port that is allowed to send VLAN 200 can send
VLAN 200, even if that tag belongs to another tenant of the same trunk.

Enforcing per-port VLAN membership is therefore the job of whoever owns the
external bridge. With NMState and a NodeNetworkConfigurationPolicy, a workload
port is restricted to one tenant tag like this:

```yaml
apiVersion: nmstate.io/v1
kind: NodeNetworkConfigurationPolicy
metadata:
  name: trunk-tenant-a
spec:
  desiredState:
    interfaces:
      - name: br-trunk
        type: linux-bridge
        state: up
        bridge:
          options:
            stp:
              enabled: false
          port:
            - name: tenanta0
              vlan:
                mode: access
                tag: 100
```

Workloads started by Kubernetes get the same shape with a bridge CNI
attachment (`"type": "bridge"`, `"bridge": "br-trunk"`, `"vlan": <tag>`), which
creates one access port per pod on its tenant's VLAN.

The trunk's `host-t-<id>` port must in turn be a trunk port for every tag it
carries (or a set of `bridge vlan add dev host-t-<id> vid <tag>` commands).

Tag uniqueness is guaranteed per host master, not cluster wide. Bridging two
different external host masters together is unsupported unless the tags are
coordinated globally by the operator.

## Validation and failure reporting

The admission webhook rejects a trunk or L2VNI change that would:

- reuse a tag inside one trunk, or across trunks sharing a host master;
- make the L2VNIs of a trunk disagree on `vxlanPort` or
  `underlayAddressFamily`;
- attach a trunk to a managed bridge.

Deleting a trunk is allowed, but the webhook returns a warning for each L2VNI
still referencing it. Those L2VNIs lose their dataplane configuration and are
reported with `DependencyFailed` and the message
`referenced L2Trunk "<name>" not found` on the nodes'
`RouterNodeConfigurationStatus`.

L2Trunks are not supported on the grout datapath; the admission webhook rejects
them there.

## Standalone L2VNIs

A trunk and a standalone L2VNI can coexist on one node, including on the same
VXLAN port: every L2VNI, trunked or not, uses an external (VNI filtering)
VXLAN device. A standalone L2VNI attached to the same external host master as a
trunk is unconstrained by the trunk's tag rules, since it uses an untagged
access port; make sure the platform's port configuration keeps it out of
tenant tag domains.
