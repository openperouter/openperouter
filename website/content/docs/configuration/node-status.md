---
weight: 65
title: "Node Status"
description: "How to check specific node's router configuration status"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

## Node Status

{{< hint warning >}}
**Work in progress**: 
{{< /hint >}}

The node router configuration status can be viewed and monitored via the RouterNodeConfigurationStatus CRD.

Each node will have an associated RouterNodeConfigurationStatus CR resource indicate this node router configuration status.
It reports the outcome of all configuration resources (e.g.: Underlay, L3VNI, L2VNI) affecting the node. 
In other words its the source of truth configuration health on that node.

Nodes:
```shell
$ kubectl get no
NAME                    STATUS   ROLES           AGE   VERSION
pe-kind-control-plane   Ready    control-plane   19h   v1.34.7
pe-kind-worker          Ready    worker          19h   v1.34.7
```

Nodes router status:
```shell
$ kubectl -n openperouter-system get routernodeconfigurationstatus
NAME                   READY   DEGRADED    AGE   
pe-kind-control-plane  True    False       19h
pe-kind-worker         True    False       19h   
```

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: RouterNodeConfigurationStatus
metadata:
  name: pe-kind-worker 
  namespace: openperouter-system
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: pe-kind-worker 
    uid: "e18de421-403c-4593-8787-8da99199ac2e"
status:
  conditions:
  - type: Ready
    status: "True"
    reason: ConfigurationSuccessful
    message: "All configuration applied successfully"
    lastTransitionTime: "2026-05-19T10:30:00Z"
  - type: Degraded
    status: "False"
    reason: ConfigurationSuccessful
    message: "All configuration applied successfully"
    lastTransitionTime: "2026-05-19T10:30:00Z"
```

### Degraded State and Per-Resource Error Reporting

When one or more configuration resources fail validation, the controller **skips** the
invalid resources and continues applying the rest. The node enters a degraded state:
`Ready=False`, `Degraded=True`, and `failedResources` lists each failed resource with
its failure reason.

Healthy resources are still applied. For example, if two L3VNIs are configured and one has an
invalid VRF name, the valid L3VNI is applied normally while the invalid one is reported as failed.

```shell
$ kubectl -n openperouter-system get routernodeconfigurationstatus
NAME                   READY   DEGRADED    AGE
pe-kind-control-plane  False   True        19h
pe-kind-worker         True    False       19h
```

```yaml
apiVersion: network.openperouter.io/v1alpha1
kind: RouterNodeConfigurationStatus
metadata:
  name: pe-kind-control-plane
  namespace: openperouter-system
status:
  conditions:
  - type: Ready
    status: "False"
    reason: ConfigurationFailed
    message: "Some resources failed validation, see status.failedResources for details"
  - type: Degraded
    status: "True"
    reason: ConfigurationFailed
    message: "Some resources failed validation, see status.failedResources for details"
  failedResources:
  - kind: L3VNI
    name: bad-vrf
    reason: ValidationFailed
    message: 'invalid vrf name for vni "bad-vrf", vrf "toolongvrfname": interface name too long'
  - kind: L2VNI
    name: orphan-l2
    reason: DependencyFailed
    message: 'no valid L3VNI for L3 domain "nonexistent"'
```

#### Failure Reasons

| Reason | Meaning |
|--------|---------|
| `ValidationFailed` | The resource itself is invalid (e.g., VRF name too long, duplicate vni, subnet overlap) |
| `DependencyFailed` | A resource this one depends on is missing or failed (e.g., L2VNI references an L3 domain with no valid L3VNI) |
| `OverlayAttachmentFailed` | Provisioning failure at the network layer (e.g., failed to create VRF or move interface) |
| `FrrConfigurationFailed` | Applying the FRR configuration failed |

#### Underlay Failures

Underlay validation failures are special: since all VNIs depend on the underlay, a bad underlay
means no configuration can be applied. The status shows `UnderlayFailed` and no resources are
provisioned. The existing FRR configuration is left as-is.

```yaml
status:
  conditions:
  - type: Ready
    status: "False"
    reason: UnderlayFailed
    message: "Underlay failed validation, existing FRR configuration left as-is"
  - type: Degraded
    status: "True"
    reason: UnderlayFailed
    message: "Underlay failed validation, existing FRR configuration left as-is"
  failedResources:
  - kind: Underlay
    name: underlay
    reason: ValidationFailed
    message: "failed to validate underlays: ..."
```

### Lifecycle
As soon the configuration controller is up, it will create RouterNodeConfigurationStatus CR per each node.
When a node is removed, the associated CR is garbage collected.
