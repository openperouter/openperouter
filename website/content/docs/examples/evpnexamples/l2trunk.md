---
weight: 32
title: "Shared VLAN Trunk"
description: "Carry two tagged L2VNIs over one host attachment with an L2Trunk"
icon: "article"
date: "2026-11-18T10:00:00+02:00"
lastmod: "2026-11-18T10:00:00+02:00"
toc: true
---

This example shows how a single host attachment can carry several tagged
tenants with an [`L2Trunk`]({{< ref "../../configuration/l2trunk.md" >}}).

## Overview

Two L2VNIs (`tenant-a` on VLAN 100 and `tenant-b` on VLAN 200) share one
`L2Trunk` named `workloads`. Both belong to the same L3 routing domain, so pods
on different tags can also reach each other through the distributed anycast
gateway.

The trunk host leg is attached to `br-trunk`, an external bridge created before
OpenPERouter is deployed. Pods join a tenant with the bridge CNI plugin, which
makes each pod port an access port of that tenant's VLAN.

### Example Setup

The full example can be found in the [project repository](https://github.com/openperouter/openperouter/examples/evpn/l2trunk) and can be deployed by running:

```bash
make docker-build demo-l2trunk
```

The script creates the external bridge on every node, deploys OpenPERouter,
admits the two tenant tags on the trunk host leg, and starts the pods.

### Pod-to-Pod Connectivity

Two pods are created on VLAN 100 and one on VLAN 200. Pods on the same tag are
bridged together; pods on different tags are routed by the VRF they share:

![Layer 2 Pod to Pod](/images/openpel2podtopod.svg)

### Verifying the trunk

The trunk identity is exposed as a label, and the devices it owns are named
after it:

```bash
kubectl get l2trunks.network.openperouter.io -n openperouter-system
# NAME        TYPE   TRUNK ID
# workloads   Host   1a2b3c4d

kubectl exec -n openperouter-system <router-pod> -c frr -- \
  ip netns exec perouter bridge vlan tunnelshow dev vxt-1a2b3c4d
kubectl exec -n openperouter-system <router-pod> -c frr -- \
  ip netns exec perouter bridge vni show dev vxt-1a2b3c4d
```

## Configuration

### OpenPERouter Configuration

One trunk, one routing domain and two tagged L2VNIs:

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
---
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
---
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: tenant-b
  namespace: openperouter-system
spec:
  vni: 120
  routingDomain:
    type: L3VNI
    l3vni:
      name: red
  trunk:
    name: workloads
    vlan:
      tag: 200
  gatewayIPs: ["192.170.2.1/24"]
```

**Configuration Notes:**

- **`hostMaster.linuxBridge.lifecycle`**: must be `External`; the platform owns
  the bridge and decides which VLANs each workload port may use.
- **`trunk.vlan.tag`**: one tag per L2VNI on a trunk. Reusing a tag inside a
  trunk is rejected by the admission webhook.
- **`gatewayIPs`**: each tag keeps its own anycast gateway. Because both L2VNIs
  share the `red` routing domain, pods on VLAN 100 can reach pods on VLAN 200.

### Host Preparation

The external bridge must exist on every node before the pods start:

```bash
for node in $(kind get nodes --name pe-kind); do
  docker exec "${node}" ip link add br-trunk type bridge
  docker exec "${node}" ip link set br-trunk type bridge vlan_filtering 1 vlan_default_pvid 0
  docker exec "${node}" ip link set br-trunk up
done
```

Once the trunk is reconciled, the tenant tags have to be admitted on its host
leg. The `prepare.sh` script does both steps. Note that `bridge vlan add` takes
one vid per invocation — a space separated list exits zero and admits only the
first one — so the command is repeated per tag:

```bash
for tag in 100 200; do
  bridge vlan add dev host-t-<trunk-id> vid "${tag}"
done
```

### Pod Configuration

Each tag gets a bridge network attachment that turns the pod's port into an
access port of that VLAN:

```yaml
apiVersion: "k8s.cni.cncf.io/v1"
kind: NetworkAttachmentDefinition
metadata:
  name: trunk-100
spec: '{
      "cniVersion": "0.3.0",
      "type": "bridge",
      "bridge": "br-trunk",
      "vlan": 100,
      "ipam": {
         "type": "static",
         "routes": [{"dst": "0.0.0.0/0", "gw": "192.170.1.1"}]
      }
    }'
```

Pods select the attachment in their `k8s.v1.cni.cncf.io/networks` annotation, as
in the [layer 2 example]({{< ref "layer2.md" >}}).

## Security Note

The trunk host leg carries whatever tag the platform allows on its bridge
ports. OpenPERouter maps tags to VNIs and does not police who sends which tag.
Restricting a workload port to its tenant tag is the external bridge owner's
job; see the [trust boundary]({{< ref "../../configuration/l2trunk.md" >}}) section
for a worked NMState example.
