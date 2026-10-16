# openperouter

![Version: 0.0.0](https://img.shields.io/badge/Version-0.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.0.0](https://img.shields.io/badge/AppVersion-v0.0.0-informational?style=flat-square)

An open implementation of a PE router.

**Homepage:** <https://github.com/openperouter/openperouter>

## Source Code

* <https://github.com/openperouter/openperouter>

## Requirements

Kubernetes: `>= 1.19.0-0`

| Repository | Name | Version |
|------------|------|---------|
|  | crds | 0.0.0 |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| crds.enabled | bool | `true` |  |
| crds.validationFailurePolicy | string | `"Fail"` |  |
| fullnameOverride | string | `""` |  |
| nameOverride | string | `""` |  |
| openperouter.affinity | object | `{}` | Affinity rules for all openperouter pods (router, controller, nodemarker, hostbridge). |
| openperouter.bgpListenLimit | int | `65535` | Maximum number of dynamic BGP sessions accepted via underlay listen ranges (FRR `bgp listen limit`, 1-65535). |
| openperouter.controller.cniCacheDir | string | `"/var/lib/openperouter/cni/cache"` | CNI cache directory for persistent CNI state across pod restarts |
| openperouter.controller.cniPluginDirs | list | `["/opt/openperouter/cni/bin/"]` | CNI plugin binary directories. The default matches the path baked into the controller image. Override only to use externally-provided binaries (e.g. host-mounted). |
| openperouter.controller.healthProbePort | int | `9081` | Health probe port for liveness and readiness checks |
| openperouter.controller.resources | object | `{}` |  |
| openperouter.datapath | string | `"kernel"` | Datapath to use for L3 forwarding. "kernel" uses the standard Linux kernel datapath; "grout" adds a DPDK-accelerated sidecar that runs alongside FRR (FRR's dplane_grout module syncs routes automatically). |
| openperouter.frr.image.pullPolicy | string | `""` |  |
| openperouter.frr.image.repository | string | `"quay.io/openperouter/openperouter"` |  |
| openperouter.frr.image.tag | string | `""` |  |
| openperouter.frr.reloader.resources | object | `{}` |  |
| openperouter.frr.reloader.vtyshTimeout | string | `""` | Timeout for vtysh commands used in health checks. Increase under heavy VNI load. Defaults to 10s. |
| openperouter.frr.resources | object | `{}` |  |
| openperouter.grout.image.pullPolicy | string | `""` |  |
| openperouter.grout.image.repository | string | `"quay.io/openperouter/router"` |  |
| openperouter.grout.image.tag | string | `"main-grout"` |  |
| openperouter.grout.maxMTU | string | `""` | Maximum MTU grout accepts on its ports (GROUT_MAX_MTU). The underlay ports take the MTU of the underlay interfaces, so it must be at least as large as the underlay MTU. Larger values grow every packet buffer. Leave it empty to use the grout default (1800). |
| openperouter.grout.resources.limits.cpu | string | `"500m"` |  |
| openperouter.grout.resources.limits.memory | string | `"2Gi"` |  |
| openperouter.grout.resources.requests.cpu | string | `"250m"` |  |
| openperouter.grout.resources.requests.memory | string | `"512Mi"` |  |
| openperouter.grout.tapUnderlay | bool | `false` | Use TAP ports for grout underlay interfaces instead of PCI ports. |
| openperouter.grout.testMode | bool | `false` | Run grout in test mode. Test mode requires no hugepages and uses dummy FIB algorithms, which keeps memory usage low at the cost of real longest prefix match lookups. Leave it disabled for a hugepage-backed dataplane, requesting hugepages under grout.resources. |
| openperouter.hostmode | bool | `false` | If true, enables host mode deployment: deploys hostbridge DaemonSet instead of router and controller, and configures nodemarker to run in webhook-only mode |
| openperouter.image.pullPolicy | string | `""` |  |
| openperouter.image.repository | string | `"quay.io/openperouter/router"` |  |
| openperouter.image.tag | string | `""` |  |
| openperouter.labels | object | `{}` |  |
| openperouter.logLevel | string | `"info"` | Controller log level. Must be one of: `debug`, `info`, `warn` or `error`. |
| openperouter.maxReconcileBackoffSeconds | int | `nil` | Maximum retry delay in seconds after failed API reconciliation. Unset uses the controller default rate limiter. |
| openperouter.nodeSelector | object | `{}` | Node selector to constrain all openperouter pods (router, controller, nodemarker, hostbridge) to nodes with matching labels. |
| openperouter.nodemarker.antiAffinity | string | `"required"` | How the replicas are spread over nodes when there is more than one: "required", "preferred", or "" for none. Added to openperouter.affinity. |
| openperouter.nodemarker.leaderElection | object | `{"leaseDuration":"15s","renewDeadline":"10s","retryPeriod":"2s"}` | Timings of the Lease the replicas elect a leader with. |
| openperouter.nodemarker.livenessProbe | object | `{"failureThreshold":6,"timeoutSeconds":5}` | Liveness probe of the nodemarker. A replica busy with a burst of admission calls can answer late, and a restart drops every call in flight. |
| openperouter.nodemarker.podDisruptionBudget | object | `{"enabled":true,"minAvailable":1}` | PodDisruptionBudget of the nodemarker. Rendered only with more than one replica: with one, minAvailable 1 would block every node drain. |
| openperouter.nodemarker.preStopSleepSeconds | int | `5` | Seconds a terminating replica keeps serving. Its endpoint is removed when termination starts, but the API server can still send it requests for a moment. 0 disables the delay; keep it below terminationGracePeriodSeconds. |
| openperouter.nodemarker.replicas | int | `2` | Replicas of the nodemarker. Every replica serves the webhooks; the replicas elect a leader through a Lease, and only the leader runs the node-index reconciler and the certificate rotator. |
| openperouter.nodemarker.resources | object | `{}` |  |
| openperouter.nodemarker.strategy | object | `{"rollingUpdate":{"maxSurge":0,"maxUnavailable":1},"type":"RollingUpdate"}` | Deployment strategy when there is more than one replica. maxSurge 0 replaces one replica at a time, which also works when only as many nodes as replicas are eligible under "required" anti-affinity. |
| openperouter.ovsRunDir | string | `"/var/run/openvswitch"` | OVS run directory to mount. This is the directory containing the OVS socket. |
| openperouter.ovsSocketPath | string | `""` | OVS database socket path. Defaults to standard OVS location if not specified. |
| openperouter.podAnnotations | object | `{}` |  |
| openperouter.priorityClassName | string | `""` |  |
| openperouter.runtimeClassName | string | `""` |  |
| openperouter.serviceAccounts.annotations | object | `{}` |  |
| openperouter.serviceAccounts.controller.name | string | `""` |  |
| openperouter.serviceAccounts.create | bool | `true` |  |
| openperouter.serviceAccounts.nodemarker.name | string | `""` |  |
| openperouter.serviceAccounts.perouter.name | string | `""` |  |
| openperouter.tolerations | list | `[{"effect":"NoSchedule","key":"node-role.kubernetes.io/master","operator":"Exists"},{"effect":"NoSchedule","key":"node-role.kubernetes.io/control-plane","operator":"Exists"}]` | Tolerations for all openperouter pods (router, controller, nodemarker, hostbridge). Defaults to tolerating control-plane/master node taints so the pods can run there. |
| openperouter.updateStrategy.type | string | `"RollingUpdate"` |  |
| rbac.create | bool | `true` |  |
| webhook.enabled | bool | `true` |  |
| webhook.timeoutSeconds | string | `nil` | timeoutSeconds of every validating webhook (1-30). Unset keeps the API server's default of 10 s. With failurePolicy Fail, a call that reaches a replica that no longer answers is refused only after this long. |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
