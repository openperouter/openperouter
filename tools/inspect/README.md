# Inspect OpenPERouter deployment

The `inspect` tool makes debugging OpenPERouter deployments easier by collecting related objects and logs.

## Prerequisites
- Cluster API client set for the target cluster.
- Read access to the OpenPERouter namespace; exec access to router pods for node logs.

## How to use:

### Options
| Option         | Description                                                       | Default                |
|----------------|-------------------------------------------------------------------|------------------------|
| `--namespace`  | OpenPERouter namespace                                            | `openperouter-system`  |
| `--dest-dir`   | Output directory path                                             | `openperouter-inspect` |
| `--k8s-client` | Kubernetes client                                                 | `kubectl`              |
| `--since`      | Collect pod logs newer then relative duration (e.g.: 5s, 10m, 2h) |                        |
| `-h`, `--help` | Print usage instructions                                          |                        |

**Note:** Options must be specified with `=`.

### Example:
```bash 
$ cd tools/inspect     
$ ./inspect

# override parameters
$ ./inspect --dest-dir=mydir --namespace=myns --k8s-client=oc --since=3m

# via repository make target, artifacts stored at /tmp/openperouter-inspect
$ make inspect

# override parameters
$ KUBECONFIG_PATH=$KUBECONFIG \
    make inspect \
      NAMESPACE=my-namespace \
      KUBECTL=oc \
      INSPECT_DIR=./art \
      SINCE=3m
```

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
    │   └── red-110.yaml
    ├── l3vnis
    │   └── red-100.yaml
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

## Inspect OpenPERouter nodes when running on systemd mode

When OpenPERouter runs on systemd mode, related info cannot be collected via cluster API as the router container is not
managed by the cluster.

`inspect_host` can be used for collecting related info by executing the script directly on the target node.

Artifacts are stored at the host (default is `/openperouter-inspect-host`), and can be copied to base station for inspection.

### Prerequisites
- SSH access to the target node.How to use:

### How to use:

#### Example:
```bash
# via ssh
$ ssh <target node> -- bash <<< $(cat tools/inspect/inspect_host)
$ scp -r <target node>/openperouter-inspect-host ./<target node>-perouter-inspect

# troubleshooting kind cluster node running OpenPERouter on host mode
$ docker exec pe-kind-worker -i bash <<< $(cat tools/inspect/inspect_host)
$ docker cp pe-kind-worker:/openperouter-inspect-host ./pe-kind-worker-inspect-host

# via repository make target, artifacts stored at /tmp/openperouter-systemd-mode-inspect
$ make inspect-systemd-mode
```

### Output
- `router_info_podman_quadlet.log` - router infrastructure information collected via the router Podman Quadlet container
- `root_netns_info.log` - Root network namespace information
- `configs/` - Contains collected static config resources in YAML form
- `config_files.log` - Static config resources collection log

#### Example:
```bash
$ kubectl get no
NAME                    STATUS   ROLES           AGE     VERSION
pe-kind-control-plane   Ready    control-plane   3h20m   v1.32.2
pe-kind-worker          Ready    <none>          3h20m   v1.32.2

$ make inspect-systemd-mode

$ tree /tmp/openperouter-systemd-mode-inspect/ 
├── pe-kind-control-plane
│   ├── config_files.log
│   ├── root_netns_info.log
│   ├── router_info_podman_quadlet.log
│   └── configs
│       └── node-config.yaml
└── pe-kind-worker
    ├── config_files.log
    ├── root_netns_info.log
    ├── router_info_podman_quadlet.log
    └── configs
        └── node-config.yaml
     
# inspect a specific node     
$ make inspect-systemd-mode NODES=pe-kind-worker
```
