# Inspect OpenPERouter deployment

The inspect tool makes debugging OpenPERouter deployments easier by collecting related objects and logs.

## Prerequisites
- Kubernetes client and KUBECONFIG set for the target cluster.
- Read access to the OpenPERouter namespace; exec access to router pods for node logs.


## How to use:
```bash
$ ./inspect

# overriding artifacts directory path
$ ./inspect --dest-dir=/tmp/perouter-logs

# use a different Kubernetes client
$ ./inspect --dest-dir=mydir --k8s-client=oc
```
**Note:** Options must be specified with `=`.

### Options
| Option | Default | Description |
|--------|---------|-------------|
| `--namespace` | `openperouter-system` | OpenPERouter namespace to inspect |
| `--dest-dir` | `openperouter-inspect` | Output directory path |
| `--k8s-client` | `kubectl` | Kubernetes client |
| `-h`, `--help` | | Print usage instructions |

## Output
The produced artifact directory structure:
- `overview/all.log` - Namespace resources summary
- `resources/` - Per-resource YAML (CRDs, workloads, events, configmaps, etc..)
- `pod_logs/` - Container logs for all pods
- `node_logs/` - Router networking and FRR state per node
- `timestamp` - Contains execution timestamp

Example:
```bash
$ tree ./openperouter-inspect/
./openperouter-inspect/
├── openperouter-system
│   ├── node_logs
│   │   ├── pe-kind-control-plane
│   │   │   └── router_state.log
│   │   └── pe-kind-worker
│   │       └── router_state.log
│   ├── overview
│   │   └── all.log
│   ├── pod_logs
│   │   ├── controller-4qfk6_controller.log
│   │   ├── controller-qps2z_controller.log
│   │   ├── nodemarker-7cf554c5b8-r6hrv_nodemarker.log
│   │   ├── router-9zg7w_frr.log
│   │   ├── router-9zg7w_reloader.log
│   │   ├── router-mkjhm_frr.log
│   │   └── router-mkjhm_reloader.log
│   └── resources
│       ├── configmaps
│       │   ├── frr-startup.yaml
│       │   └── kube-root-ca.crt.yaml
│       ├── daemonsets
│       │   ├── controller.yaml
│       │   └── router.yaml
│       ├── deployments
│       │   └── nodemarker.yaml
│       ├── pods
│       │   ├── controller-4qfk6.yaml
│       │   ├── controller-qps2z.yaml
│       │   ├── nodemarker-7cf554c5b8-r6hrv.yaml
│       │   ├── router-9zg7w.yaml
│       │   └── router-mkjhm.yaml
│       ├── rolebindings
│       │   ├── controller-rolebinding.yaml
│       │   └── perouter-rolebinding.yaml
│       ├── roles
│       │   ├── controller-role.yaml
│       │   └── perouter-role.yaml
│       ├── routernodeconfigurationstatuses
│       │   ├── pe-kind-control-plane.yaml
│       │   └── pe-kind-worker.yaml
│       ├── serviceaccounts
│       │   ├── controller.yaml
│       │   ├── default.yaml
│       │   └── perouter.yaml
│       └── services
│           └── openpe-webhook-service.yaml
└── timestamp
```
