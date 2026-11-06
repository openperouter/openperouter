---
weight: 35
title: "Layer 2 SR-IOV with Grout"
description: "Connect DPDK applications across an EVPN overlay using Grout and the SR-IOV Network Operator"
icon: "article"
date: "2026-11-05T00:00:00+01:00"
lastmod: "2026-11-05T00:00:00+01:00"
toc: true
---

This example connects DPDK applications on different Kubernetes nodes to the
same Layer 2 EVPN overlay. Unlike the [Layer 2 Integration
example]({{< relref "layer2.md" >}}), workloads use SR-IOV Virtual Functions
(VFs) rather than macvlan interfaces and a host bridge.

OpenPERouter uses the [Grout datapath]({{< relref "../../configuration/grout.md" >}})
and an `L2Trunk` with `spec.sriov` to attach a dedicated VF to the overlay. The
[SR-IOV Network Operator](https://github.com/k8snetworkplumbingwg/sriov-network-operator)
creates the VFs. The first two VFs are reserved for Grout/OpenPERouter;
only the remaining VFs are made available to workloads through Multus.

## Overview

On each node, one Physical Function (PF) provides eight VFs:

| Setting | Value |
|---------|-------|
| Nodes | `worker-a` and `worker-b` |
| Workload PF on each node | `enp1s0` |
| Reserved for Grout/OpenPERouter | VF indexes `0` and `1` |
| Grout trunk | VF index `0`, with no VF VLAN configured |
| Grout underlay | VF index `1`, kernel interface `enp1s0v1` |
| Workload resource pool | VF indexes `2` through `7` |
| Workload VLAN | `100` |
| Overlay VNI | `10100` |

The workload VFs are access ports for VLAN 100: applications send and receive
untagged Ethernet frames, while the NIC adds or removes the VLAN tag. The
Grout VF is a trunk port carrying tagged frames. OpenPERouter maps VLAN 100
on this trunk to VNI 10100.

```text
worker-a                                      worker-b
DPDK application                              DPDK application
    | workload VF (VLAN 100)                      | workload VF (VLAN 100)
    |                                             |
NIC embedded switch                           NIC embedded switch
    | trunk VF 0 (tagged VLAN 100)                 | trunk VF 0 (tagged VLAN 100)
    |                                             |
Grout -- VLAN 100 <-> VNI 10100 == VXLAN == VNI 10100 <-> VLAN 100 -- Grout
```

The NIC switches traffic between local workload VFs. Traffic to a remote
workload passes through the trunk VF to Grout, which encapsulates it in
VXLAN and sends it over the underlay through VF 1. FRR exchanges EVPN
routes between the nodes through the fabric.

This example is **east-west Layer 2 only**. The `L2VNI` has no
`routingDomain` or `gatewayIPs`, so no `L3VNI` or default gateway is needed.

## Prerequisites

- Two SR-IOV-capable worker nodes with a NIC and DPDK PMD supporting VF VLANs,
  local VF-to-VF switching, and delivery of workload traffic to a trusted
  trunk VF. Check these capabilities for your NIC and firmware.
- Multus and the SR-IOV Network Operator installed, including its device
  plugin and SR-IOV CNI plugin.
- OpenPERouter installed with the Grout datapath, and a fabric configured to
  peer with the nodes and exchange IPv4 unicast and EVPN routes. The
  `Underlay` resource below configures the node side of these sessions and
  advertises the VTEP addresses. Allow for VXLAN overhead in the fabric MTU.
- IOMMU enabled and `vfio-pci` available on both nodes for the non-bifurcated
  NIC configuration used below.
- Hugepages allocated for both Grout and the applications. With the resource
  requests below, each node needs at least 1536 MiB of free 2 MiB hugepages
  for one Grout instance and one application pod.

The manifests assume an Intel NIC (`vendor: "8086"`) with workload VFs bound
to `vfio-pci`. They are not a driver-independent recipe: bifurcated drivers
such as `mlx5_core` use a different workload policy and device setup.

Replace the node names, PF name, and operator namespace to match your
installation. Here the operator runs in `sriov-network-operator`; on
OpenShift it normally runs in `openshift-sriov-network-operator`. The resource
prefix is assumed to be `openshift.io`; adjust the pod resource names and
device environment variable if your operator uses a different prefix.

VF 0 and VF 1 belong to the same PF as the workload VFs, but have different
roles: VF 0 carries the tagged workload traffic into Grout, while VF 1
carries the untagged underlay traffic to the fabric. Do not extend workload
VLAN 100 between nodes on the physical network: cross-node connectivity
should use the VXLAN overlay, not bypass Grout. The ordinary kind
development environment does not provide the required SR-IOV hardware.

Label the two participating nodes:

```bash
kubectl label nodes worker-a worker-b openperouter.io/sriov=true
kubectl create namespace dpdk
```

## Configuration

### Enable the Grout Datapath

Use the following Helm values when installing OpenPERouter. See
[Installation]({{< ref "installation" >}}) for the Helm repository setup and
the [Grout configuration guide]({{< relref "../../configuration/grout.md" >}})
for the other datapath settings.

```yaml
# grout-values.yaml
openperouter:
  datapath: grout
  grout:
    resources:
      requests:
        memory: "2Gi"
        cpu: "4"
        hugepages-2Mi: "1Gi"
      limits:
        memory: "2Gi"
        cpu: "4"
        hugepages-2Mi: "1Gi"
```

`L2Trunk` resources of type `SRIOV` are supported only with Grout, not the
kernel datapath.

### Create the Workload VF Pool

Create this `SriovNetworkNodePolicy` in the operator's namespace:

```yaml
apiVersion: sriovnetwork.openshift.io/v1
kind: SriovNetworkNodePolicy
metadata:
  name: eastwest-dpdk
  namespace: sriov-network-operator
spec:
  resourceName: eastwest
  nodeSelector:
    openperouter.io/sriov: "true"
  priority: 10
  numVfs: 8
  nicSelector:
    vendor: "8086"
    pfNames:
      - "enp1s0#2-7"
  deviceType: vfio-pci
  isRdma: false
```

`numVfs: 8` creates VF indexes 0 through 7, but the `#2-7` range exposes only
the six workload VFs as `openshift.io/eastwest`. **VFs 0 and 1 are reserved
for Grout/OpenPERouter**: do not include either in a workload device-plugin
pool or allocate either to an application pod. This example uses VF 0 as
the L2 trunk and VF 1 as the underlay interface. OpenPERouter resolves
the trunk VF directly from `pfName` and `vfIndex`, without a device-plugin
resource request.

Wait for the operator to finish configuring both nodes before creating the
underlay, trunk, or workloads:

```bash
kubectl get sriovnetworknodestates -n sriov-network-operator
kubectl get node worker-a -o jsonpath='{.status.allocatable.openshift\.io/eastwest}{"\n"}'
kubectl get node worker-b -o jsonpath='{.status.allocatable.openshift\.io/eastwest}{"\n"}'
```

Both node states should report `Succeeded`, and each node should advertise
six workload VFs. Policy changes can drain or reboot nodes and recreate
VFs; plan these changes as maintenance rather than changing the PF while
Grout or application pods are using its VFs.

### Configure the Underlay

Use reserved VF 1 for the underlay. The example assumes its kernel network
interface is named `enp1s0v1` on both nodes; verify the name with
`ls /sys/class/net/enp1s0/device/virtfn1/net` and adjust `interfaceName`
below if necessary. Keep this VF bound to its kernel VF driver until
OpenPERouter takes it over, so the controller can discover its PCI address
and capture its IP configuration.

This example uses a shared underlay subnet with the following addresses:

| Endpoint | Underlay address | ASN |
|----------|------------------|-----|
| Fabric BGP neighbor | `192.0.2.1/24` | `64512` |
| `worker-a`, VF 1 | `192.0.2.2/24` | `64514` |
| `worker-b`, VF 1 | `192.0.2.3/24` | `64514` |

After VF creation and **before applying the Underlay**, configure the
interface on each host. Run on both workers:

```bash
sudo ip link set enp1s0 mtu 1600 up
sudo ip link set enp1s0 vf 1 vlan 0
sudo ip link set enp1s0v1 mtu 1600 up
```

On `worker-a`:

```bash
sudo ip address add 192.0.2.2/24 dev enp1s0v1
```

On `worker-b`:

```bash
sudo ip address add 192.0.2.3/24 dev enp1s0v1
```

The fabric must also support this MTU and have BGP neighbors for both node
addresses in AS 64514, with the IPv4 unicast and L2VPN EVPN address families
enabled. Replace the addresses, ASNs, and MTU to match your fabric. These
host commands illustrate the initial setup; persist it using your node's
network configuration tooling for deployments that must survive reboots.

Create the following resource in `openperouter-system`:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: Underlay
metadata:
  name: sriov-underlay
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      openperouter.io/sriov: "true"
  asn: 64514
  tunnelEndpoint:
    cidrs:
      - 100.65.0.0/24
  interfaces:
    - type: NetworkDevice
      networkDevice:
        interfaceName: enp1s0v1
  neighbors:
    - asn: 64512
      address: 192.0.2.1
```

Grout uses PCI underlay ports by default (`grout.tapUnderlay: false`).
OpenPERouter captures VF 1's address, binds the VF to `vfio-pci`, and
creates the Grout port `u_enp1s0v1`; the original kernel interface disappears
while Grout owns the device. `tunnelEndpoint.cidrs` supplies a unique VTEP
address for each node, separate from the link addresses above. The fabric
must provide reachability between those VTEPs and distribute their EVPN
routes. See [EVPN configuration]({{< relref "../../configuration/evpn.md" >}}).

Use this resource instead of the generic Underlay from the EVPN examples
index: multiple Underlays must not select the same node. If your nodes use
different fabric neighbor addresses, use separate Underlays with
non-overlapping node selectors.

### Create the SR-IOV Workload Network

The operator creates a `NetworkAttachmentDefinition` named `eastwest` in
the `dpdk` namespace from this `SriovNetwork`:

```yaml
apiVersion: sriovnetwork.openshift.io/v1
kind: SriovNetwork
metadata:
  name: eastwest
  namespace: sriov-network-operator
spec:
  networkNamespace: dpdk
  resourceName: eastwest
  vlan: 100
  spoofChk: "on"
  trust: "off"
  linkState: enable
  capabilities: '{"mac": true}'
  ipam: '{}'
```

- `resourceName` matches the `SriovNetworkNodePolicy` pool.
- `vlan: 100` configures the **workload** VF VLAN. It must match the
  `L2VNI.spec.trunk.vlan.tag` below, not the VXLAN VNI.
- `capabilities` allows each pod to request a known MAC address.
- Empty IPAM is intentional: a `vfio-pci` VF has no kernel network interface
  on which CNI can configure an IP address. The DPDK application manages its
  own networking; the `testpmd` validation below needs only Ethernet MACs.

Do not create a `SriovNetwork` for the trunk VF or set VLAN 100 on VF 0.
OpenPERouter does not change the PF-side settings of the trunk VF: configure
them on both workers before creating the trunk.

```bash
sudo ip link set enp1s0 vf 0 vlan 0 spoofchk off trust on
```

- `vlan 0` leaves the trunk without a VF VLAN, so it carries the tagged frames
  of the workload VFs.
- `spoofchk off` lets the trunk send frames of remote workloads, with their
  own source MAC.
- `trust on` lets the trunk receive frames for MACs it does not own.

Like the underlay VF, keep the trunk VF on its kernel VF driver until
OpenPERouter takes it over: the controller reads the VF's state from its
kernel netdev, and a VF already bound to `vfio-pci` has none. The
`eastwest-dpdk` policy covers only VF indexes 2-7, so it leaves VF 0 on the
kernel driver.

### Create the L2Trunk and L2VNI

Create these resources in `openperouter-system`:

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: L2Trunk
metadata:
  name: sriov-workloads
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      openperouter.io/sriov: "true"
  type: SRIOV
  sriov:
    pfName: enp1s0
    vfIndex: 0
---
apiVersion: network.openperouter.io/v1alpha1
kind: L2VNI
metadata:
  name: eastwest
  namespace: openperouter-system
spec:
  nodeSelector:
    matchLabels:
      openperouter.io/sriov: "true"
  vni: 10100
  trunk:
    name: sriov-workloads
    vlan:
      tag: 100
```

OpenPERouter binds VF 0 to Grout. The trunk settings configured above are
separate from the restricted workload VF settings. No
`hostMaster`, host bridge, or macvlan attachment is needed.

The `L2VNI` must reference a trunk in the same namespace, and both resources
must select the workload nodes. Each VLAN tag can be used by only one
`L2VNI` on a given trunk. Additional Layer 2 networks can share the trunk
using different VLAN tags and VNIs, with a matching `SriovNetwork` for each
workload VLAN.

### Deploy the DPDK Applications

Use an application image containing `/bin/sh`, `sleep`, and `dpdk-testpmd`
with the PMD for your NIC. **Replace the placeholder image below** before
applying the manifests. The containers initially sleep so that `testpmd`
can be started interactively during validation.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: dpdk-a
  namespace: dpdk
  annotations:
    k8s.v1.cni.cncf.io/networks: |
      [{"name": "eastwest", "mac": "02:00:00:00:01:01"}]
spec:
  nodeSelector:
    kubernetes.io/hostname: worker-a
    openperouter.io/sriov: "true"
  containers:
    - name: app
      image: registry.example.com/your-dpdk-image:tag
      command: ["sleep", "infinity"]
      securityContext:
        runAsUser: 0
        capabilities:
          add: ["IPC_LOCK"]
      resources:
        requests:
          cpu: "2"
          memory: "1Gi"
          hugepages-2Mi: "512Mi"
          openshift.io/eastwest: "1"
        limits:
          cpu: "2"
          memory: "1Gi"
          hugepages-2Mi: "512Mi"
          openshift.io/eastwest: "1"
      volumeMounts:
        - name: hugepages
          mountPath: /dev/hugepages
  volumes:
    - name: hugepages
      emptyDir:
        medium: HugePages-2Mi
---
apiVersion: v1
kind: Pod
metadata:
  name: dpdk-b
  namespace: dpdk
  annotations:
    k8s.v1.cni.cncf.io/networks: |
      [{"name": "eastwest", "mac": "02:00:00:00:01:02"}]
spec:
  nodeSelector:
    kubernetes.io/hostname: worker-b
    openperouter.io/sriov: "true"
  containers:
    - name: app
      image: registry.example.com/your-dpdk-image:tag
      command: ["sleep", "infinity"]
      securityContext:
        runAsUser: 0
        capabilities:
          add: ["IPC_LOCK"]
      resources:
        requests:
          cpu: "2"
          memory: "1Gi"
          hugepages-2Mi: "512Mi"
          openshift.io/eastwest: "1"
        limits:
          cpu: "2"
          memory: "1Gi"
          hugepages-2Mi: "512Mi"
          openshift.io/eastwest: "1"
      volumeMounts:
        - name: hugepages
          mountPath: /dev/hugepages
  volumes:
    - name: hugepages
      emptyDir:
        medium: HugePages-2Mi
```

The Multus annotation selects the generated network, while the extended
resource request allocates a workload VF from the matching pool. The device
plugin supplies its PCI address in `PCIDEVICE_OPENSHIFT_IO_EASTWEST`; do not
hard-code a workload VF index or PCI address. Neither reserved VF (0 or 1)
is attached to an application pod.

For a production application, replace the sleep command with its DPDK
startup command and configure its MAC and, if needed, IP address in the
application. Do not add VLAN 100 in the application: the NIC handles the
access-port VLAN tagging.

## Validation

### Check the VF Allocation and Overlay

```bash
kubectl get network-attachment-definitions -n dpdk eastwest
kubectl get pods -n dpdk -o wide
kubectl exec -n dpdk dpdk-a -- printenv PCIDEVICE_OPENSHIFT_IO_EASTWEST
kubectl exec -n dpdk dpdk-b -- printenv PCIDEVICE_OPENSHIFT_IO_EASTWEST
kubectl get underlays,l2trunks,l2vnis -n openperouter-system
```

Both pods should be running on different nodes, each with one allocated
PCI device. On each host, `ip link show enp1s0` should show VF 0 with no
VLAN, spoof checking off, and trust on; allocated workload VFs should have
VLAN 100 and their requested MACs.

Select the router pod on each worker and inspect Grout and FRR:

```bash
kubectl get pods -n openperouter-system -l app=router -o wide
kubectl exec -n openperouter-system <router-pod> -c grout -- grcli interface show
kubectl exec -n openperouter-system <router-pod> -c frr -- vtysh -c 'show bgp l2vpn evpn summary'
```

Grout should have the underlay PCI port `u_enp1s0v1`, a trunk PCI port,
a VLAN interface for tag 100, and a bridge domain for VNI 10100. EVPN BGP
sessions should be established.

### Send Traffic Between the Applications

The following commands assume CPUs `0-1` are available in each container.
Check `Cpus_allowed_list` in `/proc/self/status` inside each pod and replace
`-l 0-1` if necessary, especially when using the Kubernetes static CPU Manager.

In one terminal, start a receiver on `dpdk-b`:

```bash
kubectl exec -n dpdk -it dpdk-b -- /bin/sh -c \
  'exec dpdk-testpmd -l 0-1 -a "$PCIDEVICE_OPENSHIFT_IO_EASTWEST" -- -i --nb-cores=1 --forward-mode=rxonly'
```

At the `testpmd>` prompt, run:

```text
show port info all
start
```

Confirm that port 0 has MAC `02:00:00:00:01:02`. In another terminal, start
the sender with that destination MAC:

```bash
kubectl exec -n dpdk -it dpdk-a -- /bin/sh -c \
  'exec dpdk-testpmd -l 0-1 -a "$PCIDEVICE_OPENSHIFT_IO_EASTWEST" -- -i --nb-cores=1 --forward-mode=txonly --eth-peer=0,02:00:00:00:01:02'
```

Run `start` on the sender, then `show port stats all` on both applications.
The sender's TX and receiver's RX packet counters should increase. Stop
the test with `stop`, then `quit`. Repeat with the roles reversed and
`--eth-peer=0,02:00:00:00:01:01` to check the other direction.

Because the pods are on separate nodes and VLAN 100 is not extended through
the physical network, this traffic exercises the Grout VLAN-to-VXLAN path.
Check Grout counters with `grcli stats show` and FRR's learned MACs with
`vtysh -c 'show evpn mac vni 10100'` on each router after sending traffic.
Kernel `ping`, `curl`, and `tcpdump` on a pod's primary interface do not
validate the DPDK VF path; it bypasses the pod's kernel network stack.
