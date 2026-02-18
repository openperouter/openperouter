# Enhancement: BGP Route Reflector

## Summary

In cloud provider environments and hybrid deployments, the external network infrastructure often does not support EVPN address family. This prevents OpenPERouter from distributing EVPN routes between router pods via the external fabric. Without an alternative, the only option is a full-mesh iBGP topology between all router pods, which does not scale (N*(N-1)/2 sessions).

This enhancement proposes adding an optional BGP Route Reflector (RR) to handle internal EVPN route distribution between router pods. Two alternatives were considered:

1. **Standalone RR pods** (proposed): Deploy dedicated FRR pods as a Kubernetes Deployment within the cluster. Kubernetes handles scheduling and failover natively, and the RR runs with a standard FRR configuration without external dependencies.

2. **FRR-K8s based RR**: Leverage existing FRR-K8s (MetalLB FRR operator) installations to act as Route Reflectors on selected nodes. This requires using the `raw.config` feature which is explicitly marked as unsupported and experimental, adds controller complexity for node selection and failover, and creates a dependency on FRR-K8s CRD stability across upgrades.

The standalone RR pods approach is recommended because it is simpler, self-contained, production-ready, and does not depend on unsupported features. See the [Alternatives](#alternatives) section for a detailed comparison.

## Motivation

In cloud provider environments (AWS, GCP, Azure, etc.), the managed cloud router infrastructure typically does not support EVPN address family. This creates a challenge for OpenPERouter deployments that need to distribute EVPN routes between nodes:

- **Cloud routers** only support basic IPv4/IPv6 BGP, not EVPN
- **EVPN routes** cannot be distributed via the cloud network fabric
- **Full-mesh iBGP** between all router pods is unmanageable at scale (N*(N-1)/2 sessions)

A Route Reflector deployed within the cluster solves this by:
- Handling EVPN route distribution internally between router pods
- Allowing router pods to continue peering with external routers for IPv4/IPv6 connectivity

### Use Case: Cloud Provider Environments Without EVPN Support

In cloud provider environments (AWS, GCP, Azure, etc.), the cloud router infrastructure typically:

- Does not support EVPN address family
- Only provides basic BGP for external connectivity (e.g., VPN gateways, Direct Connect, Cloud Interconnect)

```
┌─────────────────────────────────────────────────────────────┐
│                    Cloud Provider Network                   │
│                                                             │
│   Cloud Router
│                                                             │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           │ eBGP (IPv4/IPv6 only, no EVPN)
                           │
┌──────────────────────────▼───────────────────────────────────┐
│                    Kubernetes Cluster                        │
│                                                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                    │
│  │ Router   │  │ Router   │  │ Router   │                    │
│  │ Pod 1    │  │ Pod 2    │  │ Pod 3    │                    │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘                    │
│       │             │             │                          │
│       │   iBGP EVPN via in-cluster RR                        │
│       │             │             │                          │
│       └─────────────┼─────────────┘                          │
│                     ▼                                        │
│              ┌─────────────┐                                 │
│              │    Route    │                                 │
│              │  Reflector  │  ← Internal only, cluster net   │
│              │             │                                 │
│              └─────────────┘                                 │
└──────────────────────────────────────────────────────────────┘
```

**The Route Reflector solves this by:**
1. Handling EVPN route distribution between router pods within the cluster
2. Router pods peer with cloud router for external IPv4/IPv6 routes (eBGP)
3. Keeping EVPN control plane internal while maintaining external connectivity

### Use Case: Hybrid Cloud Cost and Efficiency

In hybrid deployments connecting on-premises infrastructure with cloud, depending on the on-prem ToR for route reflection is costly and inefficient:

```
┌─────────────────────────────────────────────────────────────┐
│                    On-Premises DC                            │
│                                                              │
│                    ┌─────────┐                               │
│                    │   ToR   │                               │
│                    └────┬────┘                               │
│                         │                                    │
└─────────────────────────┼────────────────────────────────────┘
                          │
                          │ Cloud ↔ On-prem connectivity is
                          │ expensive and adds latency!
                          │
┌─────────────────────────▼────────────────────────────────────┐
│                    Cloud / Remote Site                        │
│                                                               │
│  Problem: East/West traffic between pods traverses on-prem   │
│                                                               │
│  ┌──────────┐  ──── ToR ────  ┌──────────┐                   │
│  │ Router   │                 │ Router   │                   │
│  │ Pod 1    │                 │ Pod 2    │                   │
│  └──────────┘                 └──────────┘                   │
└───────────────────────────────────────────────────────────────┘
```

**With in-cluster Route Reflector:**

```
┌─────────────────────────────────────────────────────────────┐
│                    On-Premises DC                            │
│                    ┌─────────┐                               │
│                    │   ToR   │ ASN 65001                     │
│                    └────┬────┘                               │
└─────────────────────────┼────────────────────────────────────┘
                          │
                          │ eBGP (EVPN + IPv4/IPv6)
                          │ North/South + external EVPN
                          │
┌─────────────────────────▼────────────────────────────────────┐
│                    Cloud / Remote Site                        │
│                                                               │
│       ┌─────────────────┼─────────────────┐                  │
│       │                 │                 │                  │
│  ┌────▼─────┐      ┌────▼─────┐      ┌────▼─────┐            │
│  │ Router   │      │ Router   │      │ Router   │ ASN 65000  │
│  │ Pod 1    │      │ Pod 2    │      │ Pod 3    │            │
│  └────┬─────┘      └────┬─────┘      └────┬─────┘            │
│       │                 │                 │                  │
│       │    iBGP EVPN (cluster network)    │                  │
│       │                 │                 │                  │
│       └─────────────────┼─────────────────┘                  │
│                         ▼                                    │
│                  ┌─────────────┐                             │
│                  │    Route    │ ASN 65000                   │
│                  │  Reflector  │ East/West EVPN only         │
│                  └─────────────┘                             │
└───────────────────────────────────────────────────────────────┘
```

**Benefits:**
1. **Reduced costs**: East/West traffic stays within the cloud, avoiding expensive cross-site connectivity
2. **Reduced latency**: East/West traffic takes direct path, not through on-prem infrastructure
3. **Control plane independence**: Internal EVPN distribution stays within the cluster
4. **Data plane independence**: VXLAN encapsulated traffic between pods doesn't traverse on-prem
5. **Fault isolation**: ToR failure only affects North/South traffic, not East/West

**Route Flow in Hybrid Scenario:**

| Route Type | Flow |
|------------|------|
| External EVPN (from ToR) | ToR → eBGP → Router Pod → iBGP → RR → reflects to other Router Pods |
| Internal EVPN (local VMs) | Router Pod → iBGP → RR → reflects to other Router Pods |
| Internal EVPN to external | Router Pod → eBGP → ToR |

### Goals

- Configure Underlay in non-EVPN-capable ToR environments without using an eBGP mesh
- Keep all east/west traffic within the cloud in hybrid environments
- Support high availability

### Non-Goals

- Expose RR peering with external routers (router pods handle external connectivity)

## Proposal

### User Stories

- **As a cluster administrator in an environment without an EVPN-capable ToR**, I want to enable east-west traffic for L2VNIs without needing to configure a full BGP mesh between all router pods.

## Design Details

### Architecture Overview

```
┌───────────────────────────────────────────────────────────────────────┐
│                         External Network                              │
│                                                                       │
│                    ┌─────────────────────┐                            │
│                    │   ToR               │                            │
│                    └──────────┬──────────┘                            │
│                               │                                       │
└───────────────────────────────┼───────────────────────────────────────┘
                                │
                       eBGP (IPv4/IPv6/EVPN)
                       Underlay network
                                │
┌───────────────────────────────┼───────────────────────────────────────┐
│                               │                      Kubernetes       │
│         ┌─────────────────────┼─────────────────────┐                 │
│         │                     │                     │                 │
│    ┌────▼─────┐         ┌─────▼────┐         ┌──────▼───┐             │
│    │ Router   │         │ Router   │         │ Router   │             │
│    │ Pod 1    │         │ Pod 2    │         │ Pod 3    │             │
│    │          │         │          │         │          │             │
│    │ Underlay │         │ Underlay │         │ Underlay │             │
│    │ Network  │         │ Network  │         │ Network  │             │
│    └────┬─────┘         └────┬─────┘         └────┬─────┘             │
│         │                    │                    │                   │
│         │      iBGP EVPN (cluster network)        │                   │
│         │                    │                    │                   │
│         └────────────────────┼────────────────────┘                   │
│                              │                                        │
│                    ┌─────────▼─────────┐                              │
│                    │                   │                              │
│              ┌─────▼─────┐       ┌─────▼─────┐                        │
│              │ RR Pod 0  │       │ RR Pod 1  │                        │
│              │           │       │           │                        │
│              │ Cluster   │       │ Cluster   │                        │
│              │ Network   │       │ Network   │                        │
│              └───────────┘       └───────────┘                        │
│                                                                       │
│              Labels: app.kubernetes.io/component=route-reflector      │
│                                                                       │
└───────────────────────────────────────────────────────────────────────┘
```

### Network Separation

| Component | Network | Purpose |
|-----------|---------|---------|
| Router Pods | Underlay (NIC migration/Multus) | External BGP, VXLAN data plane |
| RR Pods | Cluster default network | Internal iBGP EVPN distribution |

**Why RR uses cluster default network:**
- RR is purely control plane (BGP), no data plane traffic
- Avoids NIC conflicts with router pods on the same node
- Works in any Kubernetes cluster without special CNI requirements

### Connection Patterns

| Peer Type | Direction | Network | Protocol |
|-----------|-----------|---------|----------|
| Router pods → RR | Client → RR | Cluster network | iBGP |
| Router pods → External | Router → ToR | Underlay network | eBGP |

### API Changes

A new `RouteReflector` field is added to the Underlay spec:

```go
// RouteReflectorType specifies the type of route reflector to use.
type RouteReflectorType string

const (
    // RouteReflectorTypeInternal uses the internal Route Reflector pods deployed
    // in the cluster for iBGP EVPN distribution.
    RouteReflectorTypeInternal RouteReflectorType = "Internal"
)

// RouteReflectorConfig configures automatic iBGP peering with Route Reflector pods.
type RouteReflectorConfig struct {
    // Type specifies the route reflector type. Currently only "Internal" is supported.
    Type RouteReflectorType `json:"type"`
}

// UnderlaySpec defines the desired state of Underlay
type UnderlaySpec struct {
    // ... existing fields ...

    // RouteReflector configures automatic iBGP peering with Route Reflector pods.
    // When set with type "Internal", the controller will automatically discover
    // RR pod IPs and configure BGP neighbors for EVPN route reflection.
    // +optional
    RouteReflector *RouteReflectorConfig `json:"routeReflector,omitempty"`
}
```

### Helm Configuration

The Route Reflector is enabled and configured via Helm values:

```yaml
# values.yaml
routeReflector:
  enabled: true
  replicas: 2

  # BGP configuration - must match the underlay ASN for iBGP
  asn: 64514
  clusterID: "1.1.1.1"

  # Pod CIDR for dynamic peer acceptance
  # RR accepts iBGP connections from any pod in this range
  podCIDR: "10.244.0.0/16"

  # Optional: scheduling constraints
  nodeSelector: {}
  tolerations: []

  # Optional: resource limits
  resources:
    limits:
      cpu: 500m
      memory: 256Mi
    requests:
      cpu: 100m
      memory: 128Mi

  # Optional: FRR image override
  image:
    repository: quay.io/frrouting/frr
    tag: "10.3.1"
```

### Underlay Configuration

Users enable internal RR with a simple configuration:

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514  # Must match RR ASN for iBGP
  routerIDCIDR: "10.0.0.0/24"
  nics:
    - "eth1"
  evpn:
    vtepCIDR: "100.65.0.0/24"

  # External peering (eBGP)
  neighbors:
    - address: "192.168.11.2"
      asn: 64512

  # Enable internal Route Reflector
  routeReflector:
    type: Internal
```

### Controller Changes

The `PERouterReconciler` is updated to:

1. **Watch RR pods**: Monitor pods with label `app.kubernetes.io/component=route-reflector`
2. **Discover RR IPs**: When underlay has `routeReflector.type: Internal`, fetch ready RR pod IPs
3. **Add RR neighbors**: Include RR IPs as iBGP neighbors in FRR configuration
4. **React to changes**: Reconcile when RR pod IPs change (restart, reschedule)

**Key implementation details:**

The existing cache is scoped to `app=router` we will need to create a new annotation to cover boths the router pod and the
rouer reflector pods and use it at the filtering cache

### FRR Template Changes

**allowas-in for eBGP only:**

The `allowas-in` directive is only applied to eBGP neighbors (different ASN), not iBGP:

```
{{- range .Underlay.Neighbors }}
    neighbor {{ .Addr }} activate
    {{- if ne .ASN $.Underlay.MyASN }}
    neighbor {{ .Addr }} allowas-in
    {{- end }}
{{- end }}
```

**RR neighbors added automatically:**

When `routeReflector.type: Internal` is set, RR pod IPs are added as iBGP neighbors:

```go
// Add internal Route Reflector neighbors if configured
if underlay.Spec.RouteReflector != nil &&
   underlay.Spec.RouteReflector.Type == v1alpha1.RouteReflectorTypeInternal {
    for _, rrIP := range config.RouteReflectorIPs {
        rrNeighbor := frr.NeighborConfig{
            Name:     fmt.Sprintf("rr@%s", rrIP),
            ASN:      underlay.Spec.ASN, // iBGP - same ASN
            Addr:     rrIP,
            IPFamily: ipfamily.ForAddressString(rrIP),
        }
        underlayNeighbors = append(underlayNeighbors, rrNeighbor)
    }
}
```

### Kubernetes Resources

#### Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: openperouter-rr
  namespace: openperouter-system
  labels:
    app.kubernetes.io/name: openperouter-rr
    app.kubernetes.io/component: route-reflector
spec:
  replicas: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: openperouter-rr
      app.kubernetes.io/component: route-reflector
  template:
    metadata:
      labels:
        app.kubernetes.io/name: openperouter-rr
        app.kubernetes.io/component: route-reflector
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app.kubernetes.io/name: openperouter-rr
                topologyKey: kubernetes.io/hostname
      containers:
        - name: frr
          image: quay.io/frrouting/frr:10.3.1
          ports:
            - containerPort: 179
              name: bgp
              protocol: TCP
          securityContext:
            capabilities:
              add:
                - NET_ADMIN
                - NET_RAW
                - NET_BIND_SERVICE
          volumeMounts:
            - name: frr-config
              mountPath: /etc/frr
          livenessProbe:
            exec:
              command:
                - /usr/lib/frr/frrinit.sh
                - status
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            tcpSocket:
              port: 179
            initialDelaySeconds: 5
            periodSeconds: 5
      volumes:
        - name: frr-config
          configMap:
            name: route-reflector-config
```

### RR FRR Configuration

```
frr version 10.3
frr defaults traditional
hostname route-reflector
log syslog informational
service integrated-vtysh-config

router bgp {{ .Values.routeReflector.asn }}
  bgp router-id {{ .Values.routeReflector.clusterID }}
  bgp cluster-id {{ .Values.routeReflector.clusterID }}
  no bgp ebgp-requires-policy
  no bgp default ipv4-unicast

  ! Define peer-group first
  neighbor CLIENTS peer-group
  neighbor CLIENTS remote-as {{ .Values.routeReflector.asn }}

  ! Accept iBGP clients dynamically from cluster pod CIDR
  bgp listen range {{ .Values.routeReflector.podCIDR }} peer-group CLIENTS

  address-family l2vpn evpn
    neighbor CLIENTS activate
    neighbor CLIENTS route-reflector-client
  exit-address-family
```

### Complete Example

#### Step 1: Deploy Route Reflector (Helm)

```bash
helm upgrade --install openperouter ./charts/openperouter \
  --set routeReflector.enabled=true \
  --set routeReflector.asn=64514 \
  --set routeReflector.clusterID="1.1.1.1" \
  --set routeReflector.podCIDR="10.244.0.0/16"
```

#### Step 2: Verify RR is Running

```bash
$ kubectl get pods -n openperouter-system -l app.kubernetes.io/component=route-reflector
NAME                               READY   STATUS    RESTARTS   AGE
openperouter-rr-5ff7bcf47d-6p848   1/1     Running   0          5m
openperouter-rr-5ff7bcf47d-7bnqp   1/1     Running   0          5m
```

#### Step 3: Configure Underlay with Internal RR

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514
  routerIDCIDR: "10.0.0.0/24"
  nics:
    - "toswitch"
  evpn:
    vtepCIDR: "100.65.0.0/24"

  neighbors:
    - address: "192.168.11.2"
      asn: 64512

  routeReflector:
    type: Internal
```

#### Step 4: Verify BGP Sessions

```bash
$ kubectl exec -n openperouter-system router-xxx -c frr -- vtysh -c "show bgp summary"

L2VPN EVPN Summary:
BGP router identifier 10.0.0.1, local AS number 64514 VRF default vrf-id 0

Neighbor        V    AS    MsgRcvd  MsgSent  Up/Down  State/PfxRcd
10.244.1.16     4  64514       11        7  00:00:19            3
10.244.1.17     4  64514       11        7  00:00:09            3
192.168.11.2    4  64512       25       29  00:05:43            3
```

## Implementation Summary

### Files Modified

1. **api/v1alpha1/underlay_types.go** - Added `RouteReflector` field and types
2. **cmd/hostcontroller/main.go** - No cache changes; RR pods are discovered via direct API calls to avoid broadening the informer scope
3. **internal/controller/routerconfiguration/underlay_vni_controller.go** - Added RR pod discovery and watching
4. **internal/conversion/frr_conversion.go** - Added logic to include RR neighbors
5. **internal/frr/templates/*.tmpl** - Fixed `allowas-in` to only apply to eBGP neighbors
6. **config/route-reflector/route-reflector.yaml** - Deployment and ConfigMap for RR
7. **charts/openperouter/templates/route-reflector.yaml** - Helm template
8. **operator/bindata/deployment/openperouter/templates/route-reflector.yaml** - Operator template

### Key Design Decisions

1. **Deployment vs StatefulSet**: Using Deployment since pod IPs aren't stable anyway - the controller dynamically discovers current IPs
2. **Dynamic discovery**: Controller watches RR pods and updates FRR config when IPs change
3. **bgp listen range**: RR uses dynamic BGP to accept connections from any pod in the cluster CIDR
4. **iBGP with same ASN**: RR and router pods use the same ASN (configured in Helm and Underlay)
5. **allowas-in for eBGP only**: Fixed templates to not apply `allowas-in` to iBGP neighbors

## Security Considerations

1. **Network Policy**: Consider adding NetworkPolicy to restrict BGP port access to router pods only
2. **Pod Security**: RR pods need NET_ADMIN, NET_RAW, NET_BIND_SERVICE capabilities. SYS_ADMIN is not required since the RR is purely a BGP control plane component and does not need to modify sysctl parameters or perform other privileged operations.

## Alternatives

### Use FRR-K8s as Route Reflector

Instead of deploying standalone FRR pods for the Route Reflector, an alternative approach is to leverage existing FRR-K8s installations to act as Route Reflectors on selected nodes.

#### Overview

FRR-K8s (the MetalLB FRR operator) runs as a DaemonSet with `hostNetwork: true`, managing FRR instances on each node via FRRConfiguration CRDs. OpenPERouter could configure FRR-K8s on selected nodes to perform Route Reflection duties.

**No port conflict**: FRR-K8s uses `hostNetwork` (nodeIP:179), router pods use pod network (podIP:179) - different IPs, no collision.

#### How It Would Work

1. **Node selection**: OpenPERouter controller selects a pair of nodes running FRR-K8s
2. **Node labeling**: Controller labels selected nodes (e.g., `openperouter.io/route-reflector=true`)
3. **FRRConfiguration with nodeSelector**: Controller creates FRRConfiguration targeting labeled nodes
4. **Raw config for RR options**: Use `raw.config` to inject RR-specific BGP configuration
5. **Router pods peer with node IPs**: Router pods configured to peer with the node IPs of selected RR nodes

```yaml
apiVersion: frrk8s.metallb.io/v1beta1
kind: FRRConfiguration
metadata:
  name: openperouter-route-reflector
spec:
  nodeSelector:
    matchLabels:
      openperouter.io/route-reflector: "true"
  bgp:
    routers:
      - asn: 64514
  raw:
    priority: 10
    config: |
      router bgp 64514
        bgp cluster-id 1.1.1.1
        neighbor CLIENTS peer-group
        neighbor CLIENTS remote-as 64514
        bgp listen range 10.244.0.0/16 peer-group CLIENTS
        address-family l2vpn evpn
          neighbor CLIENTS activate
          neighbor CLIENTS route-reflector-client
        exit-address-family
```

#### Failover Logic

Unlike standalone RR pods where Kubernetes handles rescheduling, OpenPERouter must implement failover for FRR-K8s based RR:

1. **Monitor selected nodes**: Watch node status for NotReady conditions
2. **Detect failure**: If an RR node goes down or FRR-K8s pod fails
3. **Select replacement**: Choose a new node from available candidates
4. **Update labels**: Remove label from failed node, add to new node
5. **FRRConfiguration auto-updates**: nodeSelector causes FRR-K8s to reconfigure on new node
6. **Update router pods**: Reconfigure router pods with new RR node IP

#### Caveats

1. **RawConfig is unsupported**: The FRR-K8s API explicitly warns that the `raw.config` feature is "UNSUPPORTED and intended ONLY FOR EXPERIMENTATION. It should not be used in production environments." This is required because the FRRConfiguration CRD does not natively support:
   - `bgp cluster-id`
   - `neighbor X route-reflector-client`
   - `bgp listen range` (dynamic peers)
   - `peer-group` definitions

2. **Controller must handle failover**: Unlike Kubernetes automatically rescheduling failed pods, OpenPERouter must implement node selection and failover logic when an FRR-K8s RR node goes down.

3. **FRRConfiguration nodeSelector dependency**: This approach requires FRRConfiguration to support `nodeSelector` so OpenPERouter can label nodes dynamically and have FRR-K8s apply the RR configuration only to selected nodes.

4. **Coordination complexity**: Two systems (OpenPERouter and FRR-K8s) must be kept in sync. Changes to FRR-K8s or its CRD could break the integration.

5. **Could break with upgrades**: Since RawConfig is experimental, FRR-K8s upgrades may change behavior or remove the feature entirely.

#### Comparison

| Aspect | Standalone RR Pods | FRR-K8s RR |
|--------|-------------------|------------|
| **Failover** | Kubernetes handles pod rescheduling | Controller must implement |
| **RR Config** | Native FRR config | Unsupported RawConfig |
| **Production ready** | Yes | Experimental |
| **Dependencies** | None | FRR-K8s installed |
| **Complexity** | Simpler, isolated | More controller logic |

#### Recommendation

The standalone RR pods approach (this enhancement) is recommended due to:
- Simpler failover (leverages Kubernetes pod scheduling)
- No dependency on unsupported/experimental features
- Fewer external dependencies

The FRR-K8s alternative may become viable if FRR-K8s adds native support for Route Reflector configuration options in its CRD.

## References

- RFC 4456 - BGP Route Reflection: https://datatracker.ietf.org/doc/html/rfc4456
- FRR-K8s: https://github.com/metallb/frr-k8s
- PoC: https://github.com/qinqon/openperouter/tree/poc-rr 
