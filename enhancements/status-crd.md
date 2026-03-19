# Status Reporting and Configuration Resilience

## Summary

This enhancement proposes a status reporting system for OpenPERouter through dedicated Custom Resource Definitions (CRDs), combined with a configuration resilience mechanism that prevents a single bad configuration from compromising the entire system. The system provides visibility into per-router, per-node configuration status while enabling partial failure isolation through semantic validation.

This enhancement addresses [issue #213](https://github.com/openperouter/openperouter/issues/213).

## Motivation

Currently, operators must inspect controller logs to understand the state of OpenPERouter configurations across nodes. This creates operational challenges:

- **Limited visibility**: No API-accessible status information about underlay configuration success/failure per node
- **Troubleshooting complexity**: Interface configuration issues require log analysis across multiple controller pods
- **Monitoring gaps**: No structured way to monitor BGP session health or VNI operational status
- **Scale concerns**: Log inspection becomes impractical in large clusters with hundreds of nodes
- **Single point of failure**: A single misconfigured resource can compromise the entire OpenPERouter behavior on a node

### Goals

- Provide per-node status visibility for all OpenPERouter configurations (Underlay, L2VNI, L3VNI) through Kubernetes API
- Enable programmatic monitoring and alerting on configuration failures
- Report overall configuration health including BGP session and VNI operational status
- Maintain scalability for clusters with hundreds of nodes
- **Prevent a single bad configuration from blocking all other valid configurations**
- **Provide clear visibility into which resources failed and why**

## Proposal

### User Stories

**As a cluster administrator**, I want to quickly identify which nodes have failed configuration so I can troubleshoot network connectivity issues.

**As a monitoring system**, I want to programmatically query the configuration status across all nodes to generate alerts when any OpenPERouter configuration fails to apply.

**As a network operator**, I want to see the health of all OpenPERouter components on each node without having to check individual CRD statuses or parse controller logs.

**As an operator**, I want one misconfigured VNI to not block the configuration of other valid VNIs, so that partial failures don't cause complete outages.

**As an operator**, I want to see clearly which resources failed and why, so I can fix issues incrementally without affecting working configurations.

## Configuration Resilience Approach

To address [issue #213](https://github.com/openperouter/openperouter/issues/213), this enhancement adopts a hybrid approach combining pre-emptive semantic validation, incremental application, and failed resource tracking.

### Overview

The hybrid approach combines three strategies:

1. **Pre-emptive Semantic Validation** - Validate before applying:
   - Interface existence on the node
   - VNI conflicts (VNI IDs must be unique per node across both L2 and L3)
   - Route target overlaps
   - Dependency tree ordering
   - L3VNI dependency: must have either a healthy L2VNI in the same VRF **or** a `HostSession` configured

2. **Incremental/Isolated Application** - Apply per resource group:
   - Each L2VNI independently
   - L3VNIs with `HostSession`: applied independently after the Underlay (no L2VNI dependency)
   - L3VNIs without `HostSession`: applied after a healthy L2VNI in the same VRF succeeds

3. **Failed Resource Tracking** - Mark and skip failed resources:
   - Failed L2VNIs are recorded and skipped
   - Failed L3VNIs are recorded and skipped
   - Continue processing other resources

### Dependency Tree Model

```
                              Underlay (EVPN)
                                    |
       +----------+-----------+-----+-----+-----------+-----------+-----------+
       |          |           |           |           |           |           |
    L2VNI-A    L2VNI-B     L2VNI-F     L3VNI-C     L3VNI-D     L2VNI-E     L3VNI-G
   (VRF: red) (VRF: blue) (VRF: red) (VRF: green) (VRF: red)  (VRF: purple) (VRF: mgmt)
                                      [!! ERROR]                            [HostSession]
       |                      |                       |
       +----------------------+-----------------------+
                              |
                   L3VNI-D depends on VRF "red" existing
                   (satisfied by L2VNI-A or L2VNI-F)

                   L3VNI-C has no L2VNI for VRF "green" and no HostSession → DependencyFailed
                   L3VNI-G has HostSession → no L2VNI dependency
```

There are two types of L3VNIs:

- **L3VNI with L2VNI dependency** (e.g., L3VNI-D): No `HostSession` configured. Depends on at least one healthy L2VNI in the same VRF to provide the VRF routing domain.
- **L3VNI with HostSession** (e.g., L3VNI-G): Has `HostSession` configured, which establishes a BGP session with the host via a veth pair. Operates independently of L2VNIs — only depends on the Underlay.

**Dependency Rules:**
1. Underlay with EVPN is the root dependency for all VNIs
2. All L2VNIs depend on Underlay (can exist standalone)
3. All L3VNIs depend on Underlay
4. L3VNI without `HostSession` depends on VRF existence — satisfied if *any* L2VNI with the same VRF exists
5. L3VNI with `HostSession` has no L2VNI dependency (only depends on Underlay)
6. Multiple L2VNIs can share the same VRF
7. VNI IDs must be unique per node (across both L2 and L3)

### FRR Configuration Strategy

The system separates **validation** (per-resource, following dependency order) from **FRR application** (once per reconcile, with the full set of valid resources).

**Validation phase** — follows the dependency tree in order:

1. **Underlay**: Validate. If it fails, mark as failed, leave existing FRR config as-is, and stop.
2. **Each L2VNI**: Validate (interface exists, VNI unique). If valid, add to the valid set.
3. **L3VNI for the VRF (without HostSession)** (if one exists for this L2VNI's VRF and hasn't been validated yet): Validate and add to the valid set.
4. **Each L3VNI with HostSession**: Validate independently (depends only on Underlay). If valid, add to the valid set.
5. **Remaining L3VNIs without HostSession** whose VRF has no healthy L2VNI are marked as `DependencyFailed`.

**Application phase** — runs once after validation completes:

- Generate a single complete FRR config containing the Underlay plus all valid L2VNIs and L3VNIs.
- Apply via a single `frr-reload.py` call.

**Cleanup of previously-applied resources** is automatic: `frr-reload.py` diffs the running FRR config against the desired config and removes anything absent from the desired config. A resource that was applied in a previous reconcile but is now invalid (e.g., its interface was removed, or a VNI conflict appeared) will simply be absent from the new desired config, so `frr-reload.py` will remove it from FRR without any explicit tracking.

If a resource fails validation or application, it is marked as failed, and processing continues. On the next reconcile cycle, the resource is re-validated from scratch.

**Example traversal** (given the dependency tree above):

```
Step 1: Underlay (EVPN)
Step 2: L2VNI-A (VRF: red)
Step 3: L3VNI-D (VRF: red)         ← first healthy L2VNI for VRF "red" exists
Step 4: L2VNI-B (VRF: blue)
Step 5: L2VNI-F (VRF: red)         ← VRF "red" L3VNI already validated, skip
Step 6: L2VNI-E (VRF: purple)
Step 7: L3VNI-G (HostSession)      ← validated independently, depends only on Underlay
---
L3VNI-C (VRF: green)               ← DependencyFailed: no L2VNI for VRF "green", no HostSession
```

**Rationale:**
- Natural alignment with the dependency tree traversal order
- Failed resources don't affect previously applied resources
- Each step can be validated independently before application
- Rollback granularity: only the failed resource is skipped, not the entire config

### Failure Handling

#### Underlay Failure

If Underlay fails validation, leave the existing FRR config entirely as-is — do not push the new (invalid) desired config, and do not remove the old config. This means existing BGP sessions and VNIs remain in place even if the Underlay spec is now invalid.

This applies equally whether the failure is permanent (e.g., required interface missing from the node) or transient (e.g., reloader socket not yet available). In both cases the controller marks the Underlay as `ValidationFailed`, reports it in the status, and retries on the next reconcile.

**When the operator changes an Underlay field and the new config fails validation**: the old config stays in FRR. The status reports the failure. When the issue is resolved, the next reconcile will push the updated config.

Do not attempt to remove VNIs or clean up FRR config on Underlay failure. This simplifies implementation and avoids cascading removal complexity.

#### L2VNI/L3VNI Failures

- Use different failure reasons: `ValidationFailed` vs `DependencyFailed`
- Clear `DependencyFailed` automatically when dependency recovers
- Provide clear status messages indicating the root cause

#### ApplicationFailed

Since a single `frr-reload.py` call applies the entire valid resource set, a failure at application time is global — we cannot know which subset of resources FRR accepted. When the reload call fails:

- All resources that were in the valid set are marked as `ApplicationFailed` in the status
- FRR's running config is left unchanged from before the call (frr-reload.py is transactional: it either applies all changes or none)
- The controller retries on the next reconcile cycle with the same valid set
- If the failure is persistent, the operator can inspect the reloader logs for the root cause

`ApplicationFailed` is expected to be rare (reloader process crash, socket error). `ValidationFailed` and `DependencyFailed` are the common cases and are handled per-resource.

#### Recovery

- Re-validate failed resources on every reconcile cycle
- Clear failure status automatically when validation passes
- Resources are retried without manual intervention

## Design Details

### RouterNodeConfigurationStatus CRD

The core status reporting mechanism uses a separate CR instance for each node to report the overall configuration result. This design follows established patterns from kubernetes-nmstate and frr-k8s.

All configuration elements are processed together as a single configuration unit per node. Conflicts between CRDs or missing dependencies affect the entire configuration, making it essential to report the overall result.

#### API Structure

```go
type RouterNodeConfigurationStatusStatus struct {
    FailedResources  []FailedResource   `json:"failedResources,omitempty"`
    Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

type FailedResource struct {
    Kind      string `json:"kind"`             // "Underlay", "L2VNI", "L3VNI"
    Name      string `json:"name"`
    Reason    string `json:"reason"`           // "ValidationFailed", "DependencyFailed", "ApplicationFailed"
    Message   string `json:"message,omitempty"`
}
```

**Failure Reasons:**
- `ValidationFailed`: Resource failed pre-emptive semantic validation (e.g., interface not found, VNI conflict)
- `DependencyFailed`: Resource's dependency failed (e.g., L3VNI's VRF has no healthy L2VNI)
- `ApplicationFailed`: Resource passed validation but failed during FRR application

#### Node Association via Owner References

RouterNodeConfigurationStatus resources are associated with their target nodes through Kubernetes owner references. This provides several benefits:

- **Automatic cleanup**: Resources are automatically deleted when the associated node is removed from the cluster
- **Clear relationship**: The node association is established through standard Kubernetes metadata.

#### Standard Kubernetes Conditions

The status includes standard Kubernetes conditions to provide a consistent interface for monitoring tools:

**Condition Types:**
- `Ready`: True when all configuration is successfully applied to the node
- `Degraded`: True when some resources failed but the node is partially functional

**Condition Reasons:**
- `ConfigurationSuccessful`: All resources configured successfully
- `ConfigurationFailed`: Some resources failed, others applied successfully
- `UnderlayFailed`: Underlay failed validation, VNI configuration skipped

#### Failed Resources

When configuration failures occur, the `failedResources` field provides detailed information about which specific resources failed and why. Each failed resource includes:

- **Kind**: The type of OpenPERouter resource that failed (`Underlay`, `L2VNI`, or `L3VNI`)
- **Name**: The name of the specific resource instance
- **Reason**: Why the resource failed (`ValidationFailed`, `DependencyFailed`, `ApplicationFailed`)
- **Message**: Detailed error description explaining the failure reason

This structured approach allows operators to quickly identify problematic resources without parsing log files, and enables monitoring systems to create targeted alerts for specific failure types. Failed resources are automatically retried on each reconcile cycle and cleared when validation passes.

#### Example Resources

**Successful Configuration (all resources applied):**
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

  conditions:
  - type: Ready
    status: "True"
    reason: ConfigurationSuccessful
    message: "All configuration applied successfully"
    lastTransitionTime: "2025-01-15T10:30:00Z"
```

**Partially Applied Configuration (some resources failed):**
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

  failedResources:
    - kind: L2VNI
      name: tenant-network-a
      reason: ValidationFailed
      message: "Interface eth2 not present on node"
    - kind: L2VNI
      name: tenant-network-b
      reason: ValidationFailed
      message: "VNI 100 conflicts with L3VNI production-l3"
    - kind: L3VNI
      name: tenant-l3
      reason: DependencyFailed
      message: "No healthy L2VNI exists for VRF 'tenant'"
  conditions:
  - type: Ready
    status: "False"
    reason: ConfigurationFailed
    message: "3 resources failed, other resources applied successfully"
    lastTransitionTime: "2025-01-15T10:30:00Z"
  - type: Degraded
    status: "True"
    reason: ConfigurationFailed
    message: "Some resources failed to configure"
    lastTransitionTime: "2025-01-15T10:30:00Z"
```

**Underlay Failed (VNI configuration skipped):**
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: RouterNodeConfigurationStatus
metadata:
  name: worker-3
  namespace: openperouter-system
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: worker-3
    uid: "abcdef12-3456-7890-abcd-ef1234567890"
status:

  failedResources:
    - kind: Underlay
      name: production-underlay
      reason: ValidationFailed
      message: "Interface eth0 not present on node"
  conditions:
  - type: Ready
    status: "False"
    reason: UnderlayFailed
    message: "Underlay failed validation, existing FRR configuration left as-is"
    lastTransitionTime: "2025-01-15T10:30:00Z"
  - type: Degraded
    status: "True"
    reason: UnderlayFailed
    message: "Underlay failed validation, VNI processing skipped"
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
kubectl get routernodeconfigurationstatus -o json | jq '.items[] | select(.status.failedResources | length > 0) | {node: .metadata.name, failed: [.status.failedResources[] | "\(.kind)/\(.name): \(.message)"]}'

# List all failed resources across all nodes
kubectl get routernodeconfigurationstatus -o json | jq '[.items[] | .status.failedResources[]? | {node: .metadata.name, kind, name, reason, message}]'

# Check for underlay failures specifically
kubectl get routernodeconfigurationstatus -o json | jq '.items[] | select(.status.failedResources[]? | .kind == "Underlay") | .metadata.name'
```

Example output:
```
# Single namespace
NAME          READY   DEGRADED   AGE
worker-1      True    False      5m
worker-2      False   True       5m
worker-3      False   True       5m
control-1     True    False      5m
```

### Implementation Details

#### Controller Behavior

The OpenPERouter controller creates and manages RouterNodeConfigurationStatus resources:

1. **Creation**: Creates one RouterNodeConfigurationStatus per node when any OpenPERouter configuration is applied
2. **End-of-reconcile updates**: At the end of each reconcile, the controller computes the full status struct (conditions + failedResources) from the reconcile results, compares it with the current CRD status, and patches the CRD only if the status changed. This keeps status updates co-located with reconcile logic and avoids scattered update calls or concurrent channel complexity.
3. **Status reporting**: Reports configuration results through standard Kubernetes conditions for all OpenPERouter resources on the node

#### Reconciliation with Resilience

The controller follows this flow during reconciliation:

1. **Build dependency tree**: Identify all Underlay, L2VNI, and L3VNI resources targeting this node
2. **Validate Underlay**: If it fails, mark as failed, leave existing FRR config as-is, and stop
3. **For each L2VNI in order**:
   - Validate the L2VNI (interface exists, VNI unique)
   - If valid, add to the valid set
   - If a corresponding L3VNI without HostSession exists for this VRF (and hasn't been validated yet), validate and add to the valid set
   - If invalid, mark as failed and continue with the next L2VNI
4. **For each L3VNI with HostSession**: Validate independently (depends only on Underlay, not on any L2VNI); if valid, add to the valid set
5. **Mark remaining L3VNIs without HostSession** whose VRF has no healthy L2VNI as `DependencyFailed`
6. **Apply once**: Generate a single complete FRR config from the valid set and call `frr-reload.py` once; if the call fails, mark all resources in the valid set as `ApplicationFailed`
7. **Update status**: Record failed resources with reasons, update conditions

**Key behaviors:**
- Failed resources are re-validated on every reconcile cycle
- Failure status is cleared automatically when validation passes
- `DependencyFailed` entries are cleared when the dependency recovers
- Multiple L2VNIs can share the same VRF; L3VNI is satisfied if any L2VNI with that VRF is healthy
- Previously-applied resources that become invalid are automatically removed from FRR: since the desired config only includes valid resources, `frr-reload.py`'s diff removes anything no longer present

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

### All-or-Nothing with Rollback

**Description**: Instead of skipping individual failed resources, apply all-or-nothing semantics with rollback to the previous known-good state on failure.

#### How It Works

1. **Pre-emptive Semantic Validation** - Same validation (interface existence, VNI conflicts, etc.)
2. **Mark resources as "Validated"** - All resources that pass validation get a condition
3. **Apply all-or-nothing** - Either all new/changed resources apply successfully, or none do
4. **On error: Rollback** - Restore previous FRR config from backup
5. **Mark resources as "Degraded"** - User must fix the issue to proceed with new configurations

**Important clarification:** Neither approach blocks existing working VNIs. Rollback preserves the previous working state - only the new batch of changes being applied is affected. Existing VNIs continue to function.

#### Status Conditions (Rollback Approach)

- `Validated` - Passed semantic validation (interface exists, no VNI conflicts)
- `Applied` - Successfully configured in FRR and host
- `Degraded` - Failed at application time, system rolled back
- `ValidationFailed` - Failed semantic validation

#### Comparison

| Criteria | Skip Failed | Rollback |
|----------|-------------|----------|
| Existing VNIs | Remain functional | Remain functional (rollback preserves) |
| New batch on failure | Valid ones applied, failed skipped | Entire new batch rejected |
| User experience | Partial success possible | Must fix all issues to apply new batch |
| Implementation complexity | Higher | Lower |
| Partial state risk | Yes (mix of new valid + old) | No (clean rollback to previous state) |
| Recovery | Automatic (failed retried each cycle) | Manual (user must fix and retry) |
| State machine complexity | Complex (per-resource states) | Simple (binary success/failure) |
| Testing burden | Higher (many state combinations) | Lower (fewer scenarios) |

#### Advantages of Rollback Approach

1. **Simpler implementation** - No per-resource failure tracking, no partial state management
2. **Clear semantics** - All or nothing is easy to understand and reason about
3. **No partial state confusion** - System is either fully working with latest config or fully working with previous config
4. **Lower testing burden** - Fewer state combinations to test; binary success/failure is easier to validate
5. **Atomic changes** - Operators can be confident that either all their changes applied or none did
6. **Easier debugging** - No need to understand which subset of resources are applied vs failed

#### Disadvantages of Rollback Approach

1. **One bad config blocks new configurations** - Even unrelated VNIs in the same batch are blocked from being added
2. **Requires user action** - System won't self-heal; operator must notice and fix the issue
3. **Potential for delayed deployments** - If user doesn't notice the failure, new configurations remain pending

#### When Rollback Would Be Preferred

The rollback approach would be a better choice when:
- Simplicity is more important than partial success
- Operators actively monitor the system and respond quickly to failures
- Configuration errors are rare and quickly fixed
- Avoiding partial/inconsistent state is a priority
- The team prefers atomic, all-or-nothing deployment semantics

#### Why Skip-Failed Was Chosen

The skip-failed approach was selected for OpenPERouter because:

1. **Production availability requirements** - In production EVPN environments, one misconfigured VNI should not block the deployment of other unrelated VNIs
2. **Self-healing** - Failed resources are automatically retried on each reconcile cycle, reducing operator burden
3. **VRF isolation is natural** - VRFs are independent routing domains; a problem in one VRF shouldn't affect others
4. **Better operator experience** - Clear visibility into exactly which resources are working and which failed, with reasons
5. **Incremental recovery** - Fix one resource at a time without affecting others

## Implementation Plan

### Prerequisite: Multiple L2VNIs per VRF (issue #222)

The dependency model requires that multiple L2VNIs can share the same VRF (rule 6 in the dependency tree). The current codebase has a bug where a second L2VNI for the same VRF overwrites the first. This must be fixed before any phase begins, as the validation logic in Phase 2 depends on correctly enumerating all L2VNIs per VRF when resolving L3VNI dependencies.

**Deliverable:** Fix https://github.com/openperouter/openperouter/issues/222 — multiple L2VNIs with the same VRF are additive, not replacement.

### Phase 1: RouterNodeConfigurationStatus CRD Creation

Introduce the RouterNodeConfigurationStatus CRD and basic resource lifecycle management.

**Deliverables:**
- RouterNodeConfigurationStatus CRD definition with `FailedResource` type
- Controller logic for creating/deleting status resources per node
- Basic resource structure with status field

### Phase 2: Semantic Validation and Failure Tracking

Implement pre-emptive semantic validation and failed resource tracking.

**Deliverables:**
- Interface existence validation
- VNI uniqueness validation (per node, across L2/L3)
- Dependency tree builder (Underlay → L2VNI → L3VNI)
- Failed resource tracking with reasons
- Multiple L2VNIs per VRF support (fix existing bug https://github.com/openperouter/openperouter/issues/222)

### Phase 3: Status Reporting

Compute and persist the RouterNodeConfigurationStatus at the end of each reconcile based on the reconcile results.

**Deliverables:**
- Status struct computation at reconcile end (conditions + failedResources)
- Patch CRD status only when it differs from the current state (avoid spurious writes)
- Standard Kubernetes conditions (Ready, Degraded)
- FailedResources detailed reporting

### Phase 4: FRR Config Generation with Valid Resource Set

Implement FRR configuration generation: validate per resource following dependency order, then apply once with the full valid set.

**Deliverables:**
- Per-resource validation following dependency tree order (Underlay → L2VNI → L3VNI)
- Single complete FRR config generation from the valid resource set per reconcile
- Single `frr-reload.py` call per reconcile (diff-based application removes previously-applied resources that are now invalid)
- Underlay failure handling (leave config as-is, stop processing)
- Host interface application per valid L2VNI
- Automatic retry of failed resources on each reconcile cycle

## Benefits and Trade-offs

### Benefits

| Benefit | Description |
|---------|-------------|
| **Partial Failure Isolation** | One failing VNI does not block all others from being configured |
| **Clear Dependency Visibility** | Tree structure makes dependencies explicit and debuggable |
| **Status Transparency** | Users see exactly what is working and what failed |
| **Graceful Degradation** | System continues operating with valid resources |
| **Better Debugging** | Failed resource records provide failure history with reasons |
| **Incremental Recovery** | Fix one resource, it gets applied in next reconcile cycle |
| **Prevents Cascading Failures** | L3VNI failure doesn't affect sibling VRFs |

### Trade-offs

| Trade-off | Description |
|-----------|-------------|
| **Increased Complexity** | More code paths, state machines, harder to reason about |
| **Single frr-reload.py per reconcile** | One reload call per reconcile regardless of resource count; diff-based so previously-applied invalid resources are cleaned up automatically |
| **Status Update Load** | More Kubernetes API calls for status updates per node |
| **Partial State** | System can be in "partially configured" state (may confuse operators) |
| **Testing Complexity** | Many more failure scenarios and state combinations to test |
