# Highly Available Admission Webhooks

Related issue: [#820](https://github.com/openperouter/openperouter/issues/820).

## Summary

Make OpenPERouter admission available during planned rollouts and resilient to
individual webhook replica failures, allowing users to continue creating and
updating OpenPERouter resources.

## Motivation

The standard deployment combines admission webhooks, node-index allocation and
certificate rotation in the node-marker process.

These responsibilities have different availability requirements:

- **Admission webhooks** are on the API request path. They are registered with
  `failurePolicy: Fail`, so while no webhook endpoint is available every create
  or update of an OpenPERouter resource is rejected. They need redundant
  serving instances.
- **Node-index allocation** only acts when a node without a valid index joins
  the cluster. Indexes already assigned are stored as Node annotations and keep
  being used while the allocator is down, so only new nodes wait.
- **Certificate rotation** is not on the request path. Webhooks serve with the
  certificate already stored in the Secret, and the rotator only renews it.
  With the defaults in use the serving certificate is valid for one year and is
  renewed 90 days before expiry, so renewal can pause for a long time before
  admission is affected. The exception is the first installation, where the
  webhooks wait for the initial certificate to be generated.

Combining them makes scaling webhooks require additional coordination within the
node marker.

## Proposal

Separate the responsibilities into independently deployed components:

| Component | Replicas | Responsibility |
|-----------|----------|----------------|
| Node marker | 1 | Allocate node indexes |
| Admission webhook | 2 | Serve admission requests active/active |
| Certificate controller | 1 | Manage certificates and webhook CA bundles |

The certificate controller runs the existing upstream
[OPA cert-controller](https://github.com/open-policy-agent/cert-controller)
library. Both webhook replicas consume the shared TLS Secret through Kubernetes,
with controller-runtime handling certificate reloads.

The webhook Deployment is configured to keep two replicas serving:

- **Readiness:** a replica is ready only once its HTTPS server answers, so the
  Service never routes to a replica without a usable certificate.
- **Rollout:** `maxUnavailable: 0` and `maxSurge: 1`, so two replicas stay ready
  for the whole rollout.
- **Disruption budget:** `minAvailable: 1`, so a drain can evict one replica but
  never both.
- **Anti-affinity:** preferred, by hostname. Required anti-affinity would block
  the surge replica on a two-node cluster and prevent two replicas on a
  single-node cluster.
- **Shutdown:** a 5-second `preStop` delay lets the endpoint removal propagate
  before the process stops, then the webhook server finishes in-flight
  requests. The termination grace period covers the delay plus the webhook
  timeout.

The node marker and the certificate controller keep a single replica each, and
webhook replicas do not take part in leader election. Coordination between
overlapping node-marker or certificate-controller instances is existing behavior
that this proposal does not change. It is tracked in
[#964](https://github.com/openperouter/openperouter/issues/964).

Existing node indexes and certificate state should be preserved during migration.
During an upgrade the webhook Service keeps an existing serving endpoint until
the new webhook replicas are ready, so admission is not interrupted.

In systemd mode only the admission webhook and certificate controller Deployments
run in the cluster. The node marker is not deployed, because node indexes come
from the local node configuration.

When OpenPERouter is installed through the operator with OLM, OLM manages the
webhook certificates and CA bundles, so the certificate controller is not
deployed. The operator's existing separate webhook Deployment is the one scaled
to two replicas. The certificate controller is used only for manifest and Helm
installs.

## Alternatives

### Replicate the combined node marker

The [original two-commit proposal for #820][original-proposal] runs two combined
replicas, with leader election controlling node-index allocation and certificate
rotation while both serve webhooks.

This uses fewer pods and Deployments, but adds startup and leadership coordination
to a component already responsible for three distinct functions. Separating those
functions gives admission its own lifecycle.

### Use cert-manager

[cert-manager](https://cert-manager.io/) can replace the dedicated OPA-based
certificate controller, providing certificate issuance, renewal and CA injection.

This removes the OpenPERouter certificate-controller process but introduces
cert-manager as an installation dependency.

## Validation

Verify correct admission behavior during webhook rollouts and replica failures,
and while the marker or certificate controller restarts. Cover certificate
bootstrap, renewal and migration from existing installations.

Planned-rollout availability and recovery from abrupt failures should be evaluated
separately, since in-flight requests may fail when a replica disappears
unexpectedly.

[original-proposal]: https://github.com/openperouter/openperouter/issues/820#issuecomment-6251237177
