# Per-Node Multus Network Annotation

## Summary

This enhancement proposes adding support for different Multus network attachment definitions (NADs) for router pods running on control plane nodes versus worker nodes. This enables scenarios where control plane and worker nodes are connected to different physical networks or require different network configurations for the underlay.

## Motivation

Currently, OpenPERouter applies a single `multusNetworkAnnotation` to all router pods uniformly via the `OpenPERouter` CRD. This creates limitations for scenarios where the Multus configuration needed for control plane nodes differs from that of worker nodes. A common example is cloud provider environments, where it is standard practice to use separate network subnets for control plane and worker nodes. If the Multus configuration uses the Whereabouts CNI for IP assignment, different Multus configurations are required for control plane and worker pods.

### Goals

- Enable different Multus NAD configurations for control plane nodes versus worker nodes

### Non-Goals

- Maintain backward compatibility with existing single-NAD configurations
- Support for arbitrary node selector combinations (e.g., per-rack NADs) - this can be considered in a future enhancement
- Dynamic NAD selection based on runtime conditions
- Multiple NADs per single pod

## Proposal

### User Stories

- **As a cluster administrator**, I want control plane router pods to use a different Whereabouts IP range than worker router pods, so that each node type receives IP addresses from the appropriate subnet for its network segment.

## Design Details

### Option 1: Multiple DaemonSets (Recommended)

This approach modifies the operator to generate separate router DaemonSets for control plane and worker nodes, each with its own NAD annotation.

#### API Changes

Extend the `OpenPERouterSpec` to support per-node-role NAD configuration:

```go
// NodeSelectorType defines which nodes the router pods should run on.
// +kubebuilder:validation:Enum=Workers;WorkersAndControlPlane
type NodeSelectorType string

const (
    // NodeSelectorWorkers schedules router pods only on worker nodes.
    NodeSelectorWorkers NodeSelectorType = "Workers"
    // NodeSelectorWorkersAndControlPlane schedules router pods on both worker and control plane nodes.
    NodeSelectorWorkersAndControlPlane NodeSelectorType = "WorkersAndControlPlane"
)

// WorkersConfig defines the Multus configuration for workers-only deployments.
type WorkersConfig struct {
    // MultusNetworkAnnotation specifies the Multus network annotation for worker router pods.
    // +required
    MultusNetworkAnnotation string `json:"multusNetworkAnnotation"`
}

// WorkersAndControlPlaneConfig defines the Multus configuration when running on both node types.
// If both annotations are set to the same value, only one DaemonSet is created for all nodes.
type WorkersAndControlPlaneConfig struct {
    // ControlPlaneMultusNetworkAnnotation specifies the Multus network annotation
    // for router pods running on control plane nodes.
    // +required
    ControlPlaneMultusNetworkAnnotation string `json:"controlPlaneMultusNetworkAnnotation"`

    // WorkerMultusNetworkAnnotation specifies the Multus network annotation
    // for router pods running on worker nodes.
    // +required
    WorkerMultusNetworkAnnotation string `json:"workerMultusNetworkAnnotation"`
}

// OpenPERouterPods defines the configuration for router pod scheduling and networking.
type OpenPERouterPods struct {
    // NodeSelector determines which nodes the router pods should run on.
    // +kubebuilder:validation:Enum=Workers;WorkersAndControlPlane
    // +kubebuilder:default=WorkersAndControlPlane
    // +optional
    NodeSelector NodeSelectorType `json:"nodeSelector,omitempty"`

    // Workers contains the configuration when nodeSelector is "Workers".
    // +optional
    Workers *WorkersConfig `json:"workers,omitempty"`

    // WorkersAndControlPlane contains the configuration when nodeSelector is "WorkersAndControlPlane".
    // +optional
    WorkersAndControlPlane *WorkersAndControlPlaneConfig `json:"workersAndControlPlane,omitempty"`
}

type OpenPERouterSpec struct {
    // LogLevel sets the log level for OpenPERouter components.
    // +kubebuilder:validation:Enum=all;debug;info;warn;error;none
    // +optional
    LogLevel LogLevel `json:"logLevel,omitempty"`

    // RouterPods defines the configuration for router pod scheduling and Multus network annotations.
    // +required
    RouterPods OpenPERouterPods `json:"routerPods"`

    // ... rest of existing fields
}
```

> **Note:** The `NodeSelectorType` enum can be extended in the future to include a `Custom` value, allowing users to specify arbitrary node selectors for more advanced use cases (e.g., per-rack NADs).

#### Example Configuration

**Workers only:**

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: OpenPERouter
metadata:
  name: openperouter
  namespace: openperouter-system
spec:
  routerPods:
    nodeSelector: Workers
    workers:
      multusNetworkAnnotation: "openperouter-system/underlay-nad"
```

**Workers and control plane with different NADs (creates two DaemonSets):**

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: OpenPERouter
metadata:
  name: openperouter
  namespace: openperouter-system
spec:
  routerPods:
    nodeSelector: WorkersAndControlPlane
    workersAndControlPlane:
      controlPlaneMultusNetworkAnnotation: "openperouter-system/mgmt-nad"
      workerMultusNetworkAnnotation: "openperouter-system/compute-nad"
```

**Workers and control plane with same NAD (creates one DaemonSet):**

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: OpenPERouter
metadata:
  name: openperouter
  namespace: openperouter-system
spec:
  routerPods:
    nodeSelector: WorkersAndControlPlane
    workersAndControlPlane:
      controlPlaneMultusNetworkAnnotation: "openperouter-system/underlay-nad"
      workerMultusNetworkAnnotation: "openperouter-system/underlay-nad"
```

#### Implementation Details

##### Helm Chart Changes

Modify the router DaemonSet template to conditionally generate one or two DaemonSets based on `nodeSelector`:

**charts/openperouter/templates/router.yaml:**

```yaml
{{- if eq .Values.openperouter.nodeSelector "Workers" }}
# Single DaemonSet for worker nodes only
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ template "openperouter.fullname" . }}-router
  ...
spec:
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: {{ .Values.openperouter.workerMultusNetworkAnnotation | quote }}
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: node-role.kubernetes.io/control-plane
                    operator: DoesNotExist
{{- else if eq .Values.openperouter.nodeSelector "WorkersAndControlPlane" }}
{{- if eq .Values.openperouter.controlPlaneMultusNetworkAnnotation .Values.openperouter.workerMultusNetworkAnnotation }}
# Single DaemonSet for all nodes (same NAD for control plane and workers)
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ template "openperouter.fullname" . }}-router
  ...
spec:
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: {{ .Values.openperouter.workerMultusNetworkAnnotation | quote }}
    spec:
      tolerations:
        - effect: NoSchedule
          key: node-role.kubernetes.io/master
          operator: Exists
        - effect: NoSchedule
          key: node-role.kubernetes.io/control-plane
          operator: Exists
{{- else }}
# Control Plane DaemonSet
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ template "openperouter.fullname" . }}-router-control-plane
  ...
spec:
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: {{ .Values.openperouter.controlPlaneMultusNetworkAnnotation | quote }}
    spec:
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
      tolerations:
        - effect: NoSchedule
          key: node-role.kubernetes.io/master
          operator: Exists
        - effect: NoSchedule
          key: node-role.kubernetes.io/control-plane
          operator: Exists
---
# Worker DaemonSet
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: {{ template "openperouter.fullname" . }}-router-worker
  ...
spec:
  template:
    metadata:
      annotations:
        k8s.v1.cni.cncf.io/networks: {{ .Values.openperouter.workerMultusNetworkAnnotation | quote }}
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: node-role.kubernetes.io/control-plane
                    operator: DoesNotExist
{{- end }}
{{- end }}
```

##### Operator Changes

Modify `operator/internal/helm/chart.go` to pass the new values:

```go
func patchChartValues(envConfig envconfig.EnvConfig, crdConfig *operatorapi.OpenPERouter, valuesMap map[string]interface{}) {
    openperouterValues := map[string]interface{}{
        // ... existing values
    }

    routerPods := crdConfig.Spec.RouterPods
    openperouterValues["nodeSelector"] = string(routerPods.NodeSelector)

    switch routerPods.NodeSelector {
    case operatorapi.NodeSelectorWorkers:
        openperouterValues["workerMultusNetworkAnnotation"] = routerPods.Workers.MultusNetworkAnnotation
    case operatorapi.NodeSelectorWorkersAndControlPlane:
        openperouterValues["controlPlaneMultusNetworkAnnotation"] = routerPods.WorkersAndControlPlane.ControlPlaneMultusNetworkAnnotation
        openperouterValues["workerMultusNetworkAnnotation"] = routerPods.WorkersAndControlPlane.WorkerMultusNetworkAnnotation
    }

    valuesMap["openperouter"] = openperouterValues
}
```

##### Validation

Add validation in the operator or via webhook:

1. If `nodeSelector` is `Workers`, the `workers` field must be set
2. If `nodeSelector` is `WorkersAndControlPlane`, the `workersAndControlPlane` field must be set
3. The configuration field must match the `nodeSelector` value (e.g., setting `workers` when `nodeSelector` is `WorkersAndControlPlane` is invalid)

#### Advantages

- Standard Kubernetes pattern - uses native node selectors and affinities
- No additional webhook infrastructure required

#### Disadvantages

- Two DaemonSets to manage instead of one
- Slightly more complex Helm templates
- Controller DaemonSet would need similar treatment if it also requires NAD

### Option 2: Mutating Admission Webhook (Alternative)

This approach uses a mutating webhook to dynamically inject the appropriate NAD annotation based on the target node's role.

#### Implementation Details

##### New Webhook

Create a new mutating webhook that intercepts router pod creation:

**internal/webhooks/router_pod_webhook.go:**

```go
package webhooks

import (
    "context"
    "encoding/json"
    "net/http"

    corev1 "k8s.io/api/core/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type RouterPodMutator struct {
    Client                              client.Client
    ControlPlaneMultusNetworkAnnotation string
    WorkerMultusNetworkAnnotation       string
    decoder                             *admission.Decoder
}

func (m *RouterPodMutator) Handle(ctx context.Context, req admission.Request) admission.Response {
    pod := &corev1.Pod{}
    if err := m.decoder.Decode(req, pod); err != nil {
        return admission.Errored(http.StatusBadRequest, err)
    }

    // Only mutate router pods
    if pod.Labels["app"] != "openperouter-router" {
        return admission.Allowed("")
    }

    // Get the target node
    node := &corev1.Node{}
    if err := m.Client.Get(ctx, client.ObjectKey{Name: req.Name}, node); err != nil {
        // Node not yet known, use pod's node selector to determine
        return m.mutateBasedOnNodeSelector(pod)
    }

    // Determine if control plane or worker
    _, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]
    if !isControlPlane {
        _, isControlPlane = node.Labels["node-role.kubernetes.io/master"]
    }

    // Set the appropriate annotation
    if pod.Annotations == nil {
        pod.Annotations = make(map[string]string)
    }

    if isControlPlane {
        pod.Annotations["k8s.v1.cni.cncf.io/networks"] = m.ControlPlaneMultusNetworkAnnotation
    } else {
        pod.Annotations["k8s.v1.cni.cncf.io/networks"] = m.WorkerMultusNetworkAnnotation
    }

    marshaledPod, err := json.Marshal(pod)
    if err != nil {
        return admission.Errored(http.StatusInternalServerError, err)
    }

    return admission.PatchResponseFromRaw(req.Object.Raw, marshaledPod)
}
```

##### Webhook Configuration

**config/webhook/manifests.yaml** (add to existing):

```yaml
---
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: openperouter-mutating-webhook-configuration
webhooks:
- admissionReviewVersions:
  - v1
  clientConfig:
    service:
      name: webhook-service
      namespace: system
      path: /mutate-router-pod
  failurePolicy: Fail
  name: routerpodmutator.openperouter.io
  rules:
  - apiGroups:
    - ""
    apiVersions:
    - v1
    operations:
    - CREATE
    resources:
    - pods
  objectSelector:
    matchLabels:
      app: openperouter-router
  sideEffects: None
```

#### Advantages

- Single DaemonSet - simpler resource management
- More flexible - could be extended to support arbitrary node selectors in the future
- Leverages existing webhook infrastructure (nodemarker already runs a webhook server)

#### Disadvantages

- More complex logic - webhook must determine node role at pod creation time
- Timing issues - pod might be created before node assignment is known
- Harder to debug - annotation is set dynamically, not visible in DaemonSet spec
- Additional failure point - webhook unavailability could block pod creation
- Requires webhook to have access to node information

### Simplification: Control Plane vs Worker Only

Both options can be simplified by focusing only on the control-plane/worker distinction rather than supporting arbitrary node selectors. This covers the most common use case.

The `nodeSelector` enum provides a clear choice (defaults to `WorkersAndControlPlane`):

| `nodeSelector` | Configuration Field | Result |
|----------------|---------------------|--------|
| `Workers` | `workers.multusNetworkAnnotation` | Single DaemonSet on workers only |
| `WorkersAndControlPlane` | `workersAndControlPlane.*` (same NAD) | Single DaemonSet on all nodes |
| `WorkersAndControlPlane` | `workersAndControlPlane.*` (different NADs) | Two DaemonSets with different NADs |

This keeps the API simple and avoids the complexity of generic node selectors while still solving the primary use case.

## Future Considerations

If there is demand for more granular control (e.g., per-rack NADs), the `NodeSelectorType` enum can be extended to include a `Custom` value. This would allow users to specify arbitrary node selectors and corresponding Multus configurations for advanced use cases.

However, the control-plane/worker split covers the majority of real-world use cases and should be implemented first.

## References

- Current MultusNetworkAnnotation: `operator/api/v1alpha1/openperouter_types.go:42-44`
- Helm chart router template: `charts/openperouter/templates/router.yaml`
- Multus CNI: https://github.com/k8snetworkplumbingwg/multus-cni
- Kubernetes DaemonSet node selection: https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/#running-pods-on-select-nodes
