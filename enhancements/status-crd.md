# Status Reporting

## Summary

This enhancement proposes a status reporting system for OpenPERouter through dedicated Custom Resource Definitions (CRDs). The system provides visibility into per-router, per-node configuration status to enable effective troubleshooting and monitoring.

## Motivation

Currently, operators must inspect controller logs to understand the state of OpenPERouter configurations across nodes. This creates operational challenges:

- **Limited visibility**: No API-accessible status information about underlay configuration success/failure per node
- **Troubleshooting complexity**: Interface configuration issues require log analysis across multiple controller pods
- **Monitoring gaps**: No structured way to monitor BGP session health or VNI operational status
- **Scale concerns**: Log inspection becomes impractical in large clusters with hundreds of nodes

### Goals

- Provide per-node status visibility for all OpenPERouter configurations (Underlay, L2VNI, L3VNI) through Kubernetes API
- Enable programmatic monitoring and alerting on configuration failures
- Report overall configuration health including BGP session and VNI operational status
- Maintain scalability for clusters with hundreds of nodes

## Proposal

### User Stories

**As a cluster administrator**, I want to quickly identify which nodes have failed configuration so I can troubleshoot network connectivity issues.

**As a monitoring system**, I want to programmatically query the configuration status across all nodes to generate alerts when any OpenPERouter configuration fails to apply.

**As a network operator**, I want to see the health of all OpenPERouter components on each node without having to check individual CRD statuses or parse controller logs.

## Design Details

### RouterNodeConfigurationStatus CRD

The core status reporting mechanism uses a separate CR instance for each node to report the overall configuration result. This design follows established patterns from kubernetes-nmstate and frr-k8s.

All configuration elements are processed together as a single configuration unit per node. Conflicts between CRDs or missing dependencies affect the entire configuration, making it essential to report the overall result.

#### API Structure

```go
type RouterNodeConfigurationStatusStatus struct {
    LastUpdateTime   *metav1.Time       `json:"lastUpdateTime,omitempty"`
    FailedResources  []FailedResource   `json:"failedResources,omitempty"`
    Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

type FailedResource struct {
    Kind      string `json:"kind"`       // "Underlay", "L2VNI", "L3VNI"
    Name      string `json:"name"`
    Message   string `json:"message,omitempty"`
}
```

#### Node Association via Owner References

RouterNodeConfigurationStatus resources are associated with their target nodes through Kubernetes owner references. This provides several benefits:

- **Automatic cleanup**: Resources are automatically deleted when the associated node is removed from the cluster
- **Clear relationship**: The node association is established through standard Kubernetes metadata.

#### Standard Kubernetes Conditions

The status includes standard Kubernetes conditions to provide a consistent interface for monitoring tools:

**Condition Types:**
- `Ready`: True when all configuration is successfully applied to the node
- `Degraded`: True when some configuration failed but the node is partially functional

#### Failed Resources

When configuration failures occur, the `failedResources` field provides detailed information about which specific resources failed and why. Each failed resource includes:

- **Kind**: The type of OpenPERouter resource that failed (`Underlay`, `L2VNI`, or `L3VNI`)
- **Name**: The name of the specific resource instance
- **Message**: Detailed error description explaining the failure reason

This structured approach allows operators to quickly identify problematic resources without parsing log files, and enables monitoring systems to create targeted alerts for specific failure types.

**Condition Reasons:**
- `ConfigurationSuccessful`: All resources configured successfully
- `ConfigurationFailed`: Configuration failed

#### Example Resources

**Successful Configuration:**
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: RouterNodeConfigurationStatus
metadata:
  name: worker-1
  namespace: openperouter-system
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: worker-1
    uid: "12345678-1234-1234-1234-123456789abc"
status:
  lastUpdateTime: "2025-01-15T10:30:00Z"
  conditions:
  - type: Ready
    status: "True"
    reason: ConfigurationSuccessful
    message: "All configuration applied successfully"
    lastTransitionTime: "2025-01-15T10:30:00Z"
```

**Failed Configuration:**
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: RouterNodeConfigurationStatus
metadata:
  name: worker-2
  namespace: openperouter-system
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: worker-2
    uid: "87654321-4321-4321-4321-cba987654321"
status:
  lastUpdateTime: "2025-01-15T10:30:00Z"
  failedResources:
    - kind: Underlay
      name: production-underlay
      message: "Interface eth2 not present on node"
    - kind: L2VNI
      name: tenant-network
      message: "VNI 100 conflicts with an existing configuration"
  conditions:
  - type: Ready
    status: "False"
    reason: ConfigurationFailed
    message: "Configuration failed due to missing interfaces and conflicting VNI"
    lastTransitionTime: "2025-01-15T10:30:00Z"
  - type: Degraded
    status: "True"
    reason: ConfigurationFailed
    message: "Some resources failed to configure"
    lastTransitionTime: "2025-01-15T10:30:00Z"
```

#### Naming and Lifecycle

- **Resource naming**: `<nodeName>` format (simple node name since router identity is implicit from namespace)
- **Owner references**: RouterNodeConfigurationStatus resources are owned by their associated Node, enabling automatic cleanup when nodes are removed
- **Lifecycle management**: Created/updated by controller when any configuration changes; automatically cleaned up when the associated node is deleted or when the controller pod is removed from the node (due to node selectors, taints, or other scheduling constraints)
- **Namespace placement**: Same namespace as the router

#### Querying Patterns

```bash
# List all configuration status for the router in current namespace
kubectl get routernodeconfigurationstatus

# Check status for specific node
kubectl get routernodeconfigurationstatus worker-1

# Get status with conditions for monitoring
kubectl get routernodeconfigurationstatus -o json | jq '.items[] | {name: .metadata.name, ready: (.status.conditions[] | select(.type=="Ready") | .status)}'

# Check failed configurations
kubectl get routernodeconfigurationstatus -o json | jq '.items[] | select(.status.conditions[]? | select(.type=="Ready" and .status=="False"))'
```

Example output:
```
# Single namespace
NAME          READY   AGE
worker-1      True    5m
worker-2      False   5m
control-1     True    5m

```

### Implementation Details

#### Controller Behavior

The OpenPERouter controller creates and manages RouterNodeConfigurationStatus resources:

1. **Creation**: Creates one RouterNodeConfigurationStatus per node when any OpenPERouter configuration is applied
2. **Level-driven updates**: Uses a level-driven pattern where local status is updated and a message is sent via go channel to the controller. The controller reads the internal status and updates the CRD only when status changes, avoiding scattered status updates across the codebase
3. **Timestamp tracking**: Sets `lastUpdateTime` when configuration status changes
4. **Status reporting**: Reports configuration results through standard Kubernetes conditions for all OpenPERouter resources on the node

#### RBAC Requirements

The controller requires additional permissions:

```yaml
- apiGroups: ["openpe.openperouter.github.io"]
  resources: ["routernodeconfigurationstatuses"]
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


## Implementation Plan

### Phase 1: RouterNodeConfigurationStatus CRD Creation

Introduce the RouterNodeConfigurationStatus CRD and basic resource lifecycle management.

**Deliverables:**
- RouterNodeConfigurationStatus CRD definition
- Controller logic for creating/deleting status resources per node
- Basic resource structure with status field

### Phase 2: Status Reporting via Go Channels

Implement the level-driven pattern for status updates using Go channels to populate the RouterNodeConfigurationStatus with actual configuration results.

**Deliverables:**
- Internal status aggregation mechanism
- Go channel-based status communication pattern
- Standard Kubernetes conditions (Ready, Degraded)
- FailedResources detailed reporting
- Integration with existing Underlay, L2VNI, and L3VNI controllers
