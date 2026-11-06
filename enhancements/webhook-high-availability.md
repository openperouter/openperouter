# Highly Available Admission Webhooks

Related issue: [#820](https://github.com/openperouter/openperouter/issues/820).

## Summary

Make OpenPERouter admission available during planned rollouts and resilient to
individual webhook replica failures, allowing users to continue creating and
updating OpenPERouter resources.

## Motivation

The standard deployment combines admission webhooks, node-index allocation and
certificate rotation in the node-marker process.

These responsibilities have different availability requirements. Admission needs
redundant serving instances, while node-index allocation and certificate renewal
can tolerate temporary pauses. Combining them makes scaling webhooks require
additional coordination within the node marker.

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

Webhook availability is supported by TLS-serving readiness checks, pod
anti-affinity, a disruption budget and graceful termination. Admission timeouts
should be configurable.

Existing node indexes and certificate state should be preserved during migration.
The marker and certificate controller require single-writer ownership during
upgrades and replacement.

The design must account for the operator's existing separate webhook Deployment
and OLM certificate management.

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
bootstrap, renewal, CA replacement and migration from existing installations.

Planned-rollout availability and recovery from abrupt failures should be evaluated
separately, since in-flight requests may fail when a replica disappears
unexpectedly.

[original-proposal]: https://github.com/openperouter/openperouter/issues/820#issuecomment-6251237177
