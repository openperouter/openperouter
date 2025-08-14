# CRD Status Reporting

## Summary

This enhancement proposes atatus reporting system for OpenPERouter through dedicated Custom Resource Definitions (CRDs). The system provides visibility into per-node configuration status, BGP session health, and VNI operational state to enable effective troubleshooting and monitoring.

## Motivation

Currently, operators must inspect controller logs to understand the state of OpenPERouter configurations across nodes. This creates operational challenges:

- **Limited visibility**: No API-accessible status information about underlay configuration success/failure per node
- **Troubleshooting complexity**: Interface configuration issues require log analysis across multiple controller pods
- **Monitoring gaps**: No structured way to monitor BGP session health or VNI operational status
- **Scale concerns**: Log inspection becomes impractical in large clusters with hundreds of nodes

### Goals

- Provide per-node status visibility for Underlay configurations through Kubernetes API
- Enable programmatic monitoring and alerting on configuration failures
- Support future extensions for BGP session and VNI status reporting
- Maintain scalability for clusters with hundreds of nodes

## Proposal

### User Stories

**As a cluster administrator**, I want to quickly identify which nodes have failed underlay interface configuration so I can troubleshoot network connectivity issues.

## Design Details

### UnderlayNodeStatus CRD

The core status reporting mechanism uses a separate CRD for each node-underlay combination. This design follows established patterns from kubernetes-nmstate and frr-k8s.

#### API Structure

```go
type UnderlayNodeStatusSpec struct {
    NodeName     string `json:"nodeName"`
    UnderlayName string `json:"underlayName"`
}

type UnderlayNodeStatusStatus struct {
    LastReconciled     *metav1.Time      `json:"lastReconciled,omitempty"`
    InterfaceStatuses  []InterfaceStatus `json:"interfaceStatuses,omitempty"`
}

type InterfaceStatus struct {
    Name    string              `json:"name"`
    Status  InterfaceStatusType `json:"status"`
    Message string              `json:"message,omitempty"`
}

type InterfaceStatusType string

const (
    InterfaceStatusSuccessfullyConfigured InterfaceStatusType = "SuccessfullyConfigured"
    InterfaceStatusNotFound              InterfaceStatusType = "NotFound"
    InterfaceStatusInUse                 InterfaceStatusType = "InUse"
    InterfaceStatusError                 InterfaceStatusType = "Error"
)
```

#### Example Resource

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: UnderlayNodeStatus
metadata:
  name: production-underlay.worker-1
  namespace: openperouter-system
  ownerReferences:
    - apiVersion: openpe.openperouter.github.io/v1alpha1
      kind: Underlay
      name: production-underlay
spec:
  nodeName: worker-1
  underlayName: production-underlay
status:
  lastReconciled: "2025-01-15T10:30:00Z"
  interfaceStatuses:
    - name: eth1
      status: SuccessfullyConfigured
    - name: eth2
      status: NotFound
      message: "Interface eth2 not present on node"
```

#### Naming and Lifecycle

- **Resource naming**: `<underlayName>.<nodeName>` format ensures uniqueness
- **Owner references**: UnderlayNodeStatus resources are owned by their corresponding Underlay
- **Automatic cleanup**: Kubernetes garbage collection removes status objects when Underlay is deleted
- **Namespace placement**: Same namespace as the Underlay resource

#### Querying Patterns

```bash
# List all status for specific underlay
kubectl get underlaynodestatus -o json | jq '.items[] | select(.spec.underlayName == "production-underlay")'

# Check status for specific node
kubectl get underlaynodestatus -o json | jq '.items[] | select(.spec.nodeName == "worker-1")'

# Get status for specific underlay-node combination by name
kubectl get underlaynodestatus production-underlay.worker-1

# List all UnderlayNodeStatus resources
kubectl get underlaynodestatus
```

Example output:
```
NAME                           NODE       UNDERLAY              AGE
production-underlay.worker-1   worker-1   production-underlay   5m
production-underlay.worker-2   worker-2   production-underlay   5m
production-underlay.master-1   master-1   production-underlay   5m
dev-underlay.worker-1          worker-1   dev-underlay          2m
dev-underlay.worker-2          worker-2   dev-underlay          2m
```

### Implementation Details

#### Controller Behavior

The Underlay controller creates and manages UnderlayNodeStatus resources:

1. **Creation**: Creates one UnderlayNodeStatus per node when Underlay is created
2. **Updates**: Updates status during each reconciliation loop
3. **Timestamp tracking**: Sets `lastReconciled` on successful configuration attempts
4. **Interface status**: Reports configuration status for each specified interface

#### RBAC Requirements

The controller requires additional permissions:

```yaml
- apiGroups: ["openpe.openperouter.github.io"]
  resources: ["underlaynodestatuses"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
```

### Scalability Considerations

The separate CRD approach addresses scalability concerns:

- **API server load**: Avoids frequent updates to large objects (single Underlay with 500 node statuses)
- **Concurrent updates**: Each node status is independent, preventing update conflicts
- **Resource limits**: Individual status objects remain small and manageable
- **Query efficiency**: Node-specific queries don't require parsing large status arrays

## Alternatives

### Single Underlay Status Field

**Description**: Add status field directly to Underlay resource containing all node information.

**Rejected because**:
- **Concurrency issues**: Multiple controller instances writing to same resource
- **Scale limitations**: Single object becomes unwieldy with hundreds of nodes
- **Update efficiency**: Full object updates required for single node changes
- **Resource size**: May exceed etcd object size limits in large clusters

### Per-Node Status Annotations

**Description**: Store status information in node annotations.

**Rejected because**:
- **Permission requirements**: Requires node modification permissions
- **Query complexity**: No structured querying capabilities
- **Namespace isolation**: Breaks namespace-based access control
- **Data structure**: Annotations not suitable for complex nested data

## Future Extensions

We may introduce other CRDs or extend the UnderlayNodeStatus CRD in the future to cover node-specific status of:

- BGP session
- L2VNI and L3VNI

## Implementation Plan

### Phase 1: UnderlayNodeStatus

Introduce the CRD and basic lifecycle management. This was PoC'd in https://github.com/openperouter/openperouter/pull/109.

### Phase 2: Interface Status Reporting

Use the new CRD to report status of `interfaces` configuration. This is being PoC'd in https://github.com/openperouter/openperouter/pull/110.

### Phase 3+: BGP and VNI Extensions

We should evaluate what other per-node information we would like to surface to the user to help with troubleshooting.
