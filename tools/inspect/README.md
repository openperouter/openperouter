# Inspect OpenPERouter deployment

The `inspect` tool makes debugging OpenPERouter deployments easier by collecting related objects and logs.

## Prerequisites
- Cluster API client set for the target cluster.
- Read access to the OpenPERouter namespace; exec access to router pods for node logs.

## How to use:
```bash
$ ./inspect

# overriding output directory path
$ ./inspect --dest-dir=/tmp/perouter-logs

# use a different Kubernetes client
$ ./inspect --dest-dir=mydir --k8s-client=oc

# override openperouter namespace
$ ./inspect --dest-dir=mydir --namespace=myns --k8s-client=oc
```
**Note:** Options must be specified with `=`.

### Options
| Option         | Description                                                       | Default                |
|----------------|-------------------------------------------------------------------|------------------------|
| `--namespace`  | OpenPERouter namespace                                            | `openperouter-system`  |
| `--dest-dir`   | Output directory path                                             | `openperouter-inspect` |
| `--k8s-client` | Kubernetes client                                                 | `kubectl`              |
| `-h`, `--help` | Print usage instructions                                          |                        |

## Output
The output root directory contains the following:
- `timestamp` - Execution timestamp
- `inspect.log` - Execution log
- `node_info/` - Per node network and routing infrastructure information
- `<openperouter namesapce>/` - OpenPERouter namespace objects and workloads logs (defaults is `openperouter-system`)
- `<namespace name>/` - Per namespaces containing config resources directory (Underlay, L3VNI, L2VNI, etc.)

The OpenPERouter namespace directory structure:
- `overview/all.log` - Existing resources in summary
- `pod_logs/` - Pod logs
- `namespace.yaml` - Namespace state
- `events.yaml` - Events
- `<resource-name>/` - Per resource directory (CRDs, workloads)

### Example:
```bash
$ tree /tmp/openperouter-inspect/
├── inspect.log
├── timestamp
├── blue
│   ├── l2vnis
│   │   └── blue-111.yaml
│   └── l3vnis
│       └── blue-101.yaml
├── node_info
│   ├── pe-kind-control-plane
│   │   ├── root_netns_info.log
│   │   ├── router_info.log
│   │   └── router_netns_info.log
│   └── pe-kind-worker
│       ├── root_netns_info.log
│       ├── router_info.log
│       └── router_netns_info.log
└── openperouter-system
    ├── events.yaml
    ├── namespace.yaml
    ├── configmaps
    │   ├── frr-startup.yaml
    │   └── kube-root-ca.crt.yaml
    ├── daemonsets
    │   ├── controller.yaml
    │   └── router.yaml
    ├── deployments
    │   └── nodemarker.yaml
    ├── l2vnis
    │   └── layer2.yaml
    ├── l3vnis
    │   └── red.yaml
    ├── overview
    │   └── all.log
    ├── pod_logs
    │   ├── controller-cdkqz_controller.log
    │   ├── controller-j8tgb_controller.log
    │   ├── nodemarker-7cf554c5b8-8sq72_nodemarker.log
    │   ├── nodemarker-7cf554c5b8-8sq72_nodemarker_previous.log
    │   ├── router-w5d2t_frr.log
    │   ├── router-w5d2t_cp-frr-files.log
    │   ├── router-w5d2t_reloader.log
    │   ├── router-w8pz6_frr.log
    │   ├── router-w8pz6_cp-frr-files.log
    │   └── router-w8pz6_reloader.log
    ├── pods
    │   ├── controller-cdkqz.yaml
    │   ├── controller-j8tgb.yaml
    │   ├── nodemarker-7cf554c5b8-8sq72.yaml
    │   ├── router-w5d2t.yaml
    │   └── router-w8pz6.yaml
    ├── rolebindings
    │   ├── controller-rolebinding.yaml
    │   └── perouter-rolebinding.yaml
    ├── roles
    │   ├── controller-role.yaml
    │   └── perouter-role.yaml
    ├── routernodeconfigurationstatuses
    │   ├── pe-kind-control-plane.yaml
    │   └── pe-kind-worker.yaml
    ├── serviceaccounts
    │   ├── controller.yaml
    │   ├── default.yaml
    │   └── perouter.yaml
    ├── services
    │   └── openpe-webhook-service.yaml
    └── underlays
        └── underlay.yaml
```
