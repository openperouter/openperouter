# Status Reporting and Configuration Resilience

## Summary

This enhancement proposes a status reporting system for OpenPERouter through dedicated Custom Resource Definitions (CRDs), combined with a configuration resilience mechanism that prevents a single bad configuration from compromising the entire system. The system provides visibility into per-router, per-node configuration status while enabling partial failure isolation through semantic validation.

This enhancement addresses [issue #213](https://github.com/openperouter/openperouter/issues/213). It depends on [issue #280](https://github.com/openperouter/openperouter/issues/280) (L2VNI without VRF semantics) being resolved first, as the dependency model distinguishes between VRF-attached and disconnected L2VNIs.

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

To address [issue #213](https://github.com/openperouter/openperouter/issues/213), this enhancement adopts a hybrid approach combining pre-emptive semantic validation, single-apply with the valid resource set, and failed resource tracking.

### Overview

The hybrid approach combines three strategies:

1. **Pre-emptive Semantic Validation** - Validate before applying:
   - Interface existence on the node
   - VNI conflicts (VNI IDs must be unique per node across both L2 and L3)
   - Route target overlaps
   - Dependency tree ordering
   - L2VNI dependency: an L2VNI with `spec.vrf` or `l2gatewayips` must have a healthy L3VNI in the same VRF; an L2VNI without either is a disconnected overlay and has no L3VNI dependency
   - Disconnected L2VNI constraints: `l2gatewayips` rejected when `spec.vrf` is not set ([issue #280](https://github.com/openperouter/openperouter/issues/280))
   - **Note:** An L3VNI without `HostSession` that is not referenced by any L2VNI is useless but harmless — it is still provisioned (creates an empty VRF), not rejected

2. **Application** - Two complementary mechanisms:

   **Netlink (incremental, per-VRF):** The controller provisions netlink resources (VRFs, bridges, VXLANs, veths) in the router namespace incrementally. Each VRF and its associated resources are provisioned independently:
   - L2VNI + L3VNI in the same VRF
   - L3VNI only (e.g., with HostSession, no L2VNIs)
   - L2VNI only — disconnected overlay without VRF or gateway IPs (east-west L2 forwarding only)

   A failure provisioning one VRF's netlink resources does not affect other VRFs.

   **FRR configuration (single application):** After validation completes, generate a single FRR config containing the Underlay plus all valid L3VNIs and L2VNIs. Apply via a single `frr-reload.py` call. Resources that failed validation are excluded from the generated config.

3. **Failed Resource Tracking** - Mark and skip failed resources:
   - Failed L2VNIs are recorded and skipped
   - Failed L3VNIs are recorded and skipped
   - Continue processing other resources

### Dependency Tree Model

```
                              Underlay (EVPN)
                                    |
       +-------------+------------------+---------+--------+---------------+
       |             |                  |         |        |               |
    L3VNI-D       L3VNI-C            L3VNI-G      |     L3VNI-B            |
  (VRF: red)   (VRF: green)        (VRF: mgmt)    |   (VRF: blue)          |
      /  \     [No L2VNIs,        [HostSession]   |   [HostSession]        |
     /    \     no HostSession →                  |                        |
    /      \    useless but                       |                        |
   /        \   harmless]                         |                        |
  /          \                                    |                        |
L2VNI-A    L2VNI-F                             L2VNI-E                  L2VNI-H
(VRF: red)(VRF: red)                         (VRF: purple)            (VRF: not set →
                                             [!! No matching           disconnected
                                              L3VNI →                  overlay, east-west
                                              DependencyFailed]        only, no L3VNI
                                                                       dependency)
```

L3VNI is the **root of the VRF**: it defines the VRF routing domain. L2VNIs attach to a VRF by referencing the same VRF name as an L3VNI. L2VNIs without an explicit VRF are **disconnected overlays** — they provide east-west L2 forwarding only and have no L3VNI dependency (see [issue #280](https://github.com/openperouter/openperouter/issues/280)). There are two types of L3VNIs:

- **L3VNI with HostSession** (e.g., L3VNI-G): Has `HostSession` configured, which establishes a BGP session with the host via a veth pair. Operates independently of L2VNIs — only depends on the Underlay.
- **L3VNI without HostSession** (e.g., L3VNI-D): No `HostSession` configured. Intended to be referenced by L2VNIs in the same VRF to provide L3 routing for the VRF. An L3VNI without HostSession that is not referenced by any L2VNI is **useless but harmless** — it is still provisioned (creates an empty VRF), not rejected.
- **L3VNI without HostSession and without L2VNIs** (e.g., L3VNI-C): Still valid and provisioned. It creates an empty VRF with no L2 bridging. This is harmless and not worth rejecting — the operator may simply not have created the L2VNIs yet.

**Dependency Rules:**
1. Underlay with EVPN is the root dependency for all VNIs
2. All L3VNIs and L2VNIs depend on Underlay
3. L2VNIs **with `spec.vrf` or `l2gatewayips`** depend on a valid L3VNI with the same VRF name — an L2VNI referencing a VRF with no matching L3VNI is `DependencyFailed`
4. L2VNIs **without `spec.vrf`** are disconnected overlays — they only depend on the Underlay, not on any L3VNI (see [issue #280](https://github.com/openperouter/openperouter/issues/280)). Setting `l2gatewayips` on a disconnected L2VNI is rejected at validation time (`ValidationFailed`)
5. L3VNI without `HostSession` and without any L2VNI referencing its VRF is **useless but harmless** — it is still provisioned, not rejected
6. L3VNI with `HostSession` has no L2VNI dependency (only depends on Underlay)
7. Multiple L2VNIs can share the same VRF (same L3VNI)
8. VNI IDs must be unique per node (across both L2 and L3)

### Reconciliation Strategy

The system separates **validation** (per-resource, following dependency order), **netlink provisioning** (incremental, per-VRF), and **FRR application** (once per reconcile, with the full set of valid resources).

**Validation phase** — follows the dependency tree in order:

1. **Underlay**: Validate. If it fails, mark as failed, leave existing FRR config as-is, and stop.
2. **Each L3VNI**: Validate independently (VRF/VNI uniqueness, HostSession fields). If valid, add to the valid L3VNI set.
3. **Each L2VNI with explicit VRF**: Check that its VRF matches a valid L3VNI; if not, mark as `DependencyFailed`. Otherwise validate (interface exists, VNI unique) and add to the valid set.
4. **Each L2VNI without `spec.vrf`** (disconnected overlay): Validate that `l2gatewayips` is not set (reject with `ValidationFailed` if present). Otherwise validate (VNI unique) and add to the valid set — no L3VNI dependency required.

**Note:** An L3VNI without `HostSession` that has no L2VNI referencing its VRF is useless but harmless — it is still included in the valid set (creates an empty VRF).

**Application phase** — runs once after validation completes:

- Generate a single complete FRR config containing the Underlay plus all valid L2VNIs and L3VNIs.
- Apply via a single `frr-reload.py` call.

**Note on previously-applied resources:** A resource that was valid in a previous reconcile but is now invalid (e.g., its interface was removed, or a VNI conflict appeared) will be excluded from the new desired FRR config. However, the controller does **not** attempt to remove it from FRR's running config — doing so risks bricking the router if the removal fails. The stale config remains in FRR until the user fixes the resource (triggering a new valid config) or removes it entirely.

**Example traversal** (given the dependency tree above):

```
Step 1: Underlay (EVPN)
Step 2: L3VNI-D (VRF: red)         ← valid, added to valid L3VNI set
Step 3: L3VNI-C (VRF: green)       ← valid, no L2VNIs reference VRF "green" but harmless (empty VRF)
Step 4: L3VNI-G (VRF: mgmt)        ← valid, has HostSession
Step 5: L3VNI-B (VRF: blue)        ← valid, has HostSession
Step 6: L2VNI-A (VRF: red)         ← L3VNI-D exists for VRF "red" → valid
Step 7: L2VNI-F (VRF: red)         ← L3VNI-D exists for VRF "red" → valid
Step 8: L2VNI-E (VRF: purple)      ← no L3VNI for VRF "purple" → DependencyFailed
Step 9: L2VNI-H (VRF: not set)     ← disconnected overlay, no L3VNI dependency needed → valid
```

**Rationale:**
- Natural alignment with the sequence of dependencies
- Validation is per-resource: a failure in one does not stop others from being validated
- Single apply at the end: only resources that passed validation are included in the FRR config
- Failure isolation: only the failed resource is excluded, not the entire config

### Failure Handling

#### Underlay Failure

If Underlay fails validation, leave the existing FRR config entirely as-is — do not push the new (invalid) desired config, and do not remove the old config. This means existing BGP sessions and VNIs remain in place even if the Underlay spec is now invalid.

This applies equally whether the failure is permanent (e.g., required interface missing from the node) or transient (e.g., reloader socket not yet available). In both cases the controller marks the Underlay as `ValidationFailed`, reports it in the status, and retries on the next reconcile.

**When the operator changes an Underlay field and the new config fails validation**: the old config stays both in FRR and in the router's network namespace. The status reports the failure. When the issue is resolved, the next reconcile will push the updated config.

Do not attempt to remove VNIs or clean up FRR config on Underlay failure. This simplifies implementation and avoids cascading removal complexity.

#### L2VNI/L3VNI Failures

Beyond validation, L2VNI and L3VNI resources can fail at two distinct application stages with different blast radius. Netlink provisioning (VRFs, bridges, VXLANs, veths) is incremental and scoped to a single VRF — a failure only affects the resources in that VRF group (or the individual disconnected overlay for L2VNIs without `spec.vrf`), leaving other VRFs intact. FRR configuration application, on the other hand, is a single atomic `frr-reload.py` call covering all valid resources — a failure here is global and cannot be attributed to any individual VNI.

- Use different failure reasons: `ValidationFailed` vs `DependencyFailed`
- `ValidationFailed` examples: VNI conflict, interface not found, `l2gatewayips` set on a disconnected L2VNI (no `spec.vrf`)
- `DependencyFailed` examples: L2VNI with explicit VRF has no matching L3VNI
- Clear `DependencyFailed` automatically when dependency recovers
- Provide clear status messages indicating the root cause

#### OverlayAttachmentFailed

Netlink provisioning (VRFs, bridges, VXLANs, veths) is incremental and per-VRF. When provisioning a VRF's netlink resources in the router namespace fails:

- The resources belonging to that VRF group are marked as `OverlayAttachmentFailed` in the status
- Other VRF groups are unaffected and continue to be provisioned
- The controller retries on the next reconcile cycle

#### FrrConfigurationFailed

Since a single `frr-reload.py` call applies the entire valid resource set, a failure at FRR application time is global — we cannot attribute it to any individual resource. When the reload call fails:

- A single `FrrConfiguration` entry is added to `failedResources` with reason `FrrConfigurationFailed` (kind: `FrrConfiguration`, name: the node name)
- FRR's running config is left unchanged from before the call (frr-reload.py is transactional: it either applies all changes or none)
- The controller retries on the next reconcile cycle with the same valid set
- If the failure is persistent, the operator can inspect the reloader logs for the root cause

`FrrConfigurationFailed` is expected to be rare (reloader process crash, socket error). `OverlayAttachmentFailed` is uncommon but more granular (per-VRF). `ValidationFailed` and `DependencyFailed` are the common cases and are handled per-resource.

#### Recovery

- Re-validate failed resources on every reconcile cycle
- Clear failure status automatically when validation passes
- Resources are retried without manual intervention

## Design Details

### RouterNodeConfigurationStatus CRD

The core status reporting mechanism uses a separate CR instance for each node to report the overall configuration result. This design follows established patterns from kubernetes-nmstate and frr-k8s.

Each node has one RouterNodeConfigurationStatus resource that reports the outcome of every resource processed during reconciliation — both those that succeeded and those that failed — making it the single source of truth for configuration health on that node.

#### API Structure

```go
type RouterNodeConfigurationStatusStatus struct {
    FailedResources  []FailedResource   `json:"failedResources,omitempty"`
    Conditions       []metav1.Condition `json:"conditions,omitempty"`
}

type FailedResource struct {
    Kind      string `json:"kind"`             // "Underlay", "L2VNI", "L3VNI", "FrrConfiguration"
    Name      string `json:"name"`
    Reason    string `json:"reason"`           // "ValidationFailed", "DependencyFailed", "OverlayAttachmentFailed", "FrrConfigurationFailed"
    Message   string `json:"message,omitempty"`
}
```

**Failure Reasons:**
- `ValidationFailed`: Resource failed pre-emptive semantic validation (e.g., interface not found, VNI conflict)
- `DependencyFailed`: Resource's dependency is unmet (e.g., an L2VNI with `spec.vrf` references a VRF with no matching L3VNI)
- `OverlayAttachmentFailed`: Resource passed validation but failed during netlink provisioning of the networking logical infrastructure (VRFs, bridges, VXLANs, veths) in the router namespace
- `FrrConfigurationFailed`: Resource passed validation and netlink provisioning but the FRR configuration application (`frr-reload.py`) failed

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
- **Reason**: Why the resource failed (`ValidationFailed`, `DependencyFailed`, `OverlayAttachmentFailed`, `FrrConfigurationFailed`)
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
    - kind: L2VNI
      name: isolated-segment
      reason: ValidationFailed
      message: "l2gatewayips cannot be set on a disconnected L2VNI (no spec.vrf)"
    - kind: L2VNI
      name: orphan-network
      reason: DependencyFailed
      message: "No L3VNI exists for VRF 'tenant'"
  conditions:
  - type: Ready
    status: "False"
    reason: ConfigurationFailed
    message: "4 resources failed, other resources applied successfully"
    lastTransitionTime: "2025-01-15T10:30:00Z"
  - type: Degraded
    status: "True"
    reason: ConfigurationFailed
    message: "Some resources failed to configure"
    lastTransitionTime: "2025-01-15T10:30:00Z"
```

**Overlay Attachment Failed (netlink provisioning failure, per-VRF scope):**
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: RouterNodeConfigurationStatus
metadata:
  name: worker-4
  namespace: openperouter-system
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: worker-4
    uid: "11111111-2222-3333-4444-555555555555"
status:

  failedResources:
    - kind: L3VNI
      name: production-l3
      reason: OverlayAttachmentFailed
      message: "Failed to create VRF device 'red' in router namespace: operation not permitted"
    - kind: L2VNI
      name: tenant-network-a
      reason: OverlayAttachmentFailed
      message: "Failed to create VXLAN interface for VNI 100 in router namespace: device or resource busy"
  conditions:
  - type: Ready
    status: "False"
    reason: ConfigurationFailed
    message: "2 resources failed, other resources applied successfully"
    lastTransitionTime: "2025-01-15T10:30:00Z"
  - type: Degraded
    status: "True"
    reason: ConfigurationFailed
    message: "Some resources failed to configure"
    lastTransitionTime: "2025-01-15T10:30:00Z"
```

**FRR Configuration Failed (global scope, affects all valid resources):**
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: RouterNodeConfigurationStatus
metadata:
  name: worker-5
  namespace: openperouter-system
  ownerReferences:
  - apiVersion: v1
    kind: Node
    name: worker-5
    uid: "22222222-3333-4444-5555-666666666666"
status:

  failedResources:
    - kind: FrrConfiguration
      name: worker-5
      reason: FrrConfigurationFailed
      message: "frr-reload.py failed: unable to connect to reloader socket"
  conditions:
  - type: Ready
    status: "False"
    reason: ConfigurationFailed
    message: "FRR configuration application failed"
    lastTransitionTime: "2025-01-15T10:30:00Z"
  - type: Degraded
    status: "True"
    reason: ConfigurationFailed
    message: "FRR configuration application failed"
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

1. **Build resource list**: Identify all Underlay, L3VNI, and L2VNI resources targeting this node
2. **Validate Underlay**: If it fails, mark as failed, leave existing FRR config as-is, and stop
3. **For each L3VNI**: Validate independently (VRF/VNI uniqueness, HostSession fields); if valid, add to the valid L3VNI set; if invalid, mark as `ValidationFailed` and continue
4. **For each L2VNI with explicit VRF**:
   - Check that its VRF matches a valid L3VNI; if not, mark as `DependencyFailed` and continue
   - Validate the L2VNI (interface exists, VNI unique); if invalid, mark as `ValidationFailed` and continue
   - If valid, add to the valid L2VNI set
5. **For each L2VNI without `spec.vrf`** (disconnected overlay):
   - If `l2gatewayips` is set, mark as `ValidationFailed` (gateway requires a VRF) and continue
   - Validate the L2VNI (VNI unique); if invalid, mark as `ValidationFailed` and continue
   - If valid, add to the valid L2VNI set — no L3VNI dependency check needed
6. **Provision netlink resources (incremental, per-VRF)**: For each valid VRF group, provision the netlink resources (VRF device, bridges, VXLANs, veths) in the router namespace independently:
   - L2VNI + L3VNI in the same VRF
   - L3VNI only (e.g., with HostSession)
   - L2VNI only (disconnected overlay, no VRF or gateway IPs)

   A failure provisioning one VRF's netlink resources marks those resources as `OverlayAttachmentFailed` but does not affect other VRFs.
7. **Generate and apply FRR config (single application)**: Generate a single complete FRR config from the valid L3VNI and L2VNI sets (those that also passed netlink provisioning) and call `frr-reload.py` once; if the call fails, add a single `FrrConfiguration` entry to `failedResources` with reason `FrrConfigurationFailed`
8. **Update status**: Record failed resources with reasons, update conditions

**Key behaviors:**
- Failed resources are re-validated on every reconcile cycle
- Failure status is cleared automatically when validation passes
- `DependencyFailed` entries are cleared when the dependency recovers
- Multiple L2VNIs can share the same VRF (same L3VNI)
- An L3VNI without HostSession and without any L2VNIs is useless but harmless — still provisioned as an empty VRF
- An L2VNI whose referenced L3VNI later passes validation will be re-evaluated on the next reconcile
- Netlink provisioning is per-VRF: a failure in one VRF does not block others
- Previously-applied resources that become invalid are excluded from the desired FRR config but **not** actively removed from FRR's running config (to avoid bricking the router if the removal fails)

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

The dependency model requires that multiple L2VNIs can share the same VRF (rule 7 in the dependency tree). The current codebase has a bug where a second L2VNI for the same VRF overwrites the first. This must be fixed before any phase begins, as the validation logic in Phase 2 depends on correctly enumerating all L2VNIs per VRF when resolving L3VNI dependencies.

**Deliverable:** Fix https://github.com/openperouter/openperouter/issues/222 — multiple L2VNIs with the same VRF are additive, not replacement.

### Prerequisite: L2VNI without VRF semantics (issue #280)

The dependency model distinguishes between L2VNIs with an explicit VRF (which depend on a matching L3VNI) and L2VNIs without `spec.vrf` (disconnected overlays that only depend on the Underlay). The current codebase silently defaults the VRF name to the L2VNI's `metadata.name`, which conflates these two cases and can produce broken configurations (gateway IPs with no routing, VRF name collisions with L3VNIs). This must be fixed before Phase 2, as the validation and dependency resolution logic depends on being able to distinguish VRF-less L2VNIs from VRF-attached ones.

**Deliverable:** Fix https://github.com/openperouter/openperouter/issues/280 — L2VNIs without `spec.vrf` become disconnected overlays (east-west only), `l2gatewayips` without a VRF is rejected, and VRF names are cross-validated between L2VNIs and L3VNIs.

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
- Dependency tree builder (Underlay → L3VNI → L2VNI, with disconnected L2VNIs as Underlay-only dependents)
- Disconnected L2VNI validation (`l2gatewayips` rejected without explicit VRF)
- Failed resource tracking with reasons
- Multiple L2VNIs per VRF support (prerequisite: https://github.com/openperouter/openperouter/issues/222)
- L2VNI without VRF semantics (prerequisite: https://github.com/openperouter/openperouter/issues/280)

### Phase 3: Status Reporting

Compute and persist the RouterNodeConfigurationStatus at the end of each reconcile based on the reconcile results.

**Deliverables:**
- Status struct computation at reconcile end (conditions + failedResources)
- Patch CRD status only when it differs from the current state (avoid spurious writes)
- Standard Kubernetes conditions (Ready, Degraded)
- FailedResources detailed reporting

### Phase 4: Netlink Provisioning and FRR Config Generation with Valid Resource Set

Implement incremental netlink provisioning and FRR configuration generation: validate per resource following dependency order, provision netlink per-VRF, then apply FRR config once with the full valid set.

**Deliverables:**
- Per-resource validation following dependency tree order (Underlay → L3VNI → L2VNI)
- Incremental netlink provisioning per-VRF (VRF device, bridges, VXLANs, veths); failure in one VRF does not block others
- Single complete FRR config generation from the valid resource set per reconcile (only resources that also passed netlink provisioning)
- Single `frr-reload.py` call per reconcile with valid resources only (stale config from previously-valid resources is left in FRR until the user fixes or removes the resource)
- Underlay failure handling (leave config as-is, stop processing)
- Automatic retry of failed resources on each reconcile cycle

