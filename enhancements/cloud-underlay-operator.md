# Cloud Underlay Operator for Router Pod IPs

## Summary

A new operator (`github.com/openperouter/cloud-underlay-controller`) that discovers the underlay IP assigned to each router pod and registers it with the cloud provider (e.g., GCP alias IP, AWS ENI secondary IP, Azure NIC secondary IP). This makes the underlay IP routable on the cloud virtual network without manual intervention.

The design uses a pluggable cloud provider interface inspired by OpenShift's [cloud-network-config-controller](https://github.com/openshift/cloud-network-config-controller). GCP is the first implementation; AWS and Azure follow.

## Motivation

On cloud platforms, IPs used by a VM must be registered with the cloud networking layer to be routable. Router pod underlay IPs are unknown to the cloud provider, so cross-node and external traffic cannot reach them.

| Cloud Provider | IP Registration Mechanism |
|----------------|--------------------------|
| **GCP** | Alias IP ranges on NIC |
| **AWS** | Secondary private IPs on ENI |
| **Azure** | Secondary IP configurations on NIC |

Today this requires manual `gcloud`/`aws`/`az` commands per node, repeated on every pod restart or node scale event.

### Goals

- Cloud provider interface abstracting IP registration, with GCP as the first backend
- Automatically register/deregister router pod underlay IPs with the cloud provider, IPAM-agnostic
- CRD-based configuration (`CloudUnderlay`) with per-provider settings
- No modifications to existing Underlay CRD, NADs, or IPAM configuration
- Designed for future AWS and Azure provider implementations

### Non-Goals

- Managing cloud infrastructure prerequisites (subnet creation, secondary ranges, ENI creation)
- Constraining the IPAM plugin choice
- Implementing AWS/Azure/OpenStack providers in this initial phase
- Managing cloud firewall rules, Cloud Router/NCC, or route tables

## Proposal

### Architecture

The operator lives in its own repository (`github.com/openperouter/cloud-underlay-controller`) and is deployed as a Deployment alongside OpenPERouter. It watches router pods and `CloudUnderlay` CRs, and delegates cloud API calls to a pluggable provider interface.

```
┌──────────────────────────────────────────────────────────────────┐
│ Kubernetes Cluster                                               │
│                                                                  │
│  ┌───────────────┐           ┌────────────────────────────────┐  │
│  │ CloudUnderlay │  watches  │ cloud-underlay-controller      │  │
│  │ CRD           │◄─────────│ (Deployment)                   │  │
│  │               │          │                                 │  │
│  │ provider: gcp │          │  1. Discover underlay IP        │  │
│  │ gcp: ...      │          │  2. Parse node providerID       │  │
│  └───────────────┘          │  3. Call CloudProvider interface │  │
│                             └───┬─────────────────────────────┘  │
│  ┌───────────────┐  watches     │                                │
│  │ Router Pod    │◄─────────────┤                                │
│  │ underlay IP:  │              │                                │
│  │  10.0.200.1   │          ┌───┴────────┐                       │
│  └───────────────┘          │ Node       │                       │
│                             │ providerID │                       │
│                             └────────────┘                       │
└──────────────────────────────────────────────────────────────────┘
                                  │
                                  │ CloudProvider interface
                                  ▼
                  ┌────────────────────────────────────┐
                  │  ┌─────┐  ┌─────┐  ┌───────┐      │
                  │  │ GCP │  │ AWS │  │ Azure │ ...  │
                  │  └─────┘  └─────┘  └───────┘      │
                  └────────────────────────────────────┘
```

### Cloud Provider Interface

Follows CNCC's `CloudProviderIntf` pattern. Three operations:

- **EnsureIPAssigned** - Register an IP on the cloud instance for a node. Idempotent.
- **EnsureIPRemoved** - Remove an IP from the cloud instance. Idempotent.
- **CleanupNode** - Remove all controller-managed IPs from a node's cloud instance.

Provider selected by `CloudUnderlay.spec.provider`:

| Provider | ProviderID Prefix | Mechanism |
|----------|-------------------|-----------|
| `gcp` | `gce://` | Alias IP ranges |
| `aws` (future) | `aws://` | ENI secondary IPs |
| `azure` (future) | `azure://` | NIC secondary IPs |

Each provider parses the node's `spec.providerID`, handles cloud-specific concurrency control (GCP fingerprints, Azure etags), and authenticates via credentials secret, workload identity, or default credentials. For GCP, the secondary range name is discovered automatically by matching the pod IP against the subnet's configured secondary ranges.

### New CRD: CloudUnderlay

```yaml
apiVersion: cloud.openperouter.github.io/v1alpha1
kind: CloudUnderlay
metadata:
  name: cloud-underlay
  namespace: openperouter-system  # co-located with OpenPERouter to watch router pods
spec:
  provider: gcp  # or "aws", "azure"

  # Per-provider config (only the matching section is used)
  gcp:
    project: "ocpstrat-1278"  # auto-detected from providerID if empty
    credentialsSecret:
      name: gcp-credentials
      key: service-account.json

  # aws:   # future
  # azure: # future

status:
  assignments:
    - node: "worker-c-76dvj.c.ocpstrat-1278.internal"
      instance: "worker-c-76dvj"
      zone: "us-central1-c"
      ip: "10.0.200.1/32"
      podName: "openperouter-router-q4mmh"
      state: "Configured"  # Pending | Configured | Error
  conditions:
    - type: Ready
      status: "True"
      reason: "AllIPsConfigured"
```

### Controller Behavior

Deployed as a Deployment. Cloud-agnostic; delegates to the selected provider.

**Underlay IP discovery:** The controller discovers which network carries the underlay IP by inspecting the OpenPERouter installation (e.g., the Underlay CRD or the NAD referenced by the router pods). In the future, the Underlay CRD status subresource (planned but not yet implemented) could expose per-node underlay IPs, providing a cleaner source. If the underlay is managed directly by OpenPERouter without Multus, the router pods should be annotated with the underlay IP so the cloud operator can read it.

**Reconciliation:** Watches router pods and `CloudUnderlay`. Reads the underlay IP, calls the provider to register it. Removes stale IPs when pods are deleted or IPs change. Updates `CloudUnderlay` status.

**Cleanup:** Uses a finalizer on router pods and tracks managed IPs via a pod annotation, ensuring cloud IPs are removed before pod deletion and that IPs from other systems are never touched.

**IPAM-agnostic:** Passively reads whatever IP was assigned to the router pod. Never assigns or modifies pod IPs.

**Relationship with OpenPERouter:** The cloud-underlay-controller reads OpenPERouter's Kubernetes resources (router pods, Underlay CRD) but has no dependency on OpenPERouter internals or its binary. FRR configuration doesn't depend on cloud IP registration, but BGP peering requires the underlay IP to be routable, which only happens after registration.

### User Stories

- Automatic cloud IP registration so BGP peering works without manual cloud CLI commands
- Automatic cleanup when router pods are deleted or rescheduled
- Status visibility via `CloudUnderlay.status` for troubleshooting
- Transparent handling of node scale-up/down
- Provider-specific options (GCP secondary ranges, AWS ENIs) without changing Underlay or NAD configuration

### Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Cloud API rate limiting | Backoff with jitter |
| Stale IPs after crash | Full reconcile on startup against cloud API |
| Race with IP assignment | Requeue until the underlay IP is discoverable |
| Overwriting other IPs | Track managed IPs via annotation; never touch others |
| Concurrent NIC modifications | Provider-specific concurrency (fingerprints, etags) |
| Credentials exposure | Prefer workload identity; secrets mounted read-only |

### Alternatives Considered

1. **Modify the IPAM plugin**: Too coupled; would need changes per IPAM plugin
2. **Mutating Webhook**: Doesn't handle the cloud API side
3. **Embed in PERouterReconciler**: Mixes FRR config with cloud concerns; different failure modes shouldn't block each other
4. **Use cloud-network-config-controller directly**: Tied to OpenShift's EgressIP workflow and `CloudPrivateIPConfig` CRD. Doesn't fit OpenPERouter's underlay model, but its multi-cloud architecture informs this design
5. **DaemonSet instead of Deployment**: Scopes each instance to its own node, but the operator only reads Kubernetes resources and calls cloud APIs - it doesn't need host access or per-node presence. A Deployment is simpler to operate
6. **Single-provider (no interface)**: Would require refactoring when adding AWS/Azure. The interface cost is low and CNCC proves the pattern
7. **Embed as a controller inside openperouter's host controller**: Reuses existing pod watches and node context. However, it couples cloud provider concerns (credentials, API rate limits, cloud-specific dependencies) into the core OpenPERouter binary, bloats the image with cloud SDKs for users who don't need cloud integration, and ties the release cadence of cloud provider support to OpenPERouter itself. A separate operator keeps the boundaries clean and lets each project evolve independently

## Implementation Phases

1. **Cloud provider interface and GCP** - New repo, interface, CRD, GCP provider, Deployment
2. **Status and observability** - CRD status reporting, pod events, metrics
3. **Robustness** - Finalizers, annotation tracking, garbage collection, tests
4. **Additional providers** - AWS and Azure implementations
