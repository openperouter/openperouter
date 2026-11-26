# Inspect OpenPERouter deployment

The `inspect` tool makes debugging OpenPERouter deployments easier by collecting related objects and logs.

## Prerequisites
- Cluster API client set for the target cluster.
- Read access to the OpenPERouter namespace; exec access to router pods for node logs.

## How to use:

### Options
| Option                   | Description                                                       | Default                |
|--------------------------|-------------------------------------------------------------------|------------------------|
| `--namespace`            | OpenPERouter namespace                                            | `openperouter-system`  |
| `--additional-namespace` | Namespace to collect core resources and pod logs from; repeat for each namespace | |
| `--dest-dir`             | Output directory path                                             | `openperouter-inspect` |
| `--k8s-client`           | Kubernetes client                                                 | `kubectl`              |
| `--since`                | Collect pod logs newer then relative duration (e.g.: 5s, 10m, 2h) |                        |
| `-h`, `--help`           | Print usage instructions                                          |                        |

**Note:** Options must be specified with `=`.

### Example:
```bash 
$ cd tools/inspect     
$ ./inspect

# override parameters
$ ./inspect --dest-dir=mydir --namespace=myns --additional-namespace=frr-k8s-system --k8s-client=oc --since=3m

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
- `node_info/` - Per node network and routing infrastructure information, and grout device state files
  (`device_state/<interface>.json`, only with the grout datapath)
- `<openperouter namespace>/` - OpenPERouter namespace objects and workload logs (defaults to `openperouter-system`)
- `<additional namespace>/` - Generic objects, events, pod manifests, and pod logs for each requested additional
  namespace; router commands are not run in additional namespaces
- `nodes.yaml` - Kubernetes node manifests
- `<namespace name>/` - Per namespaces containing config resources directory (Underlay, L2VNI, L3VNI, L3VPN, FRRConfiguration, etc.)

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
├── nodes.yaml
├── timestamp
├── node_info
│   ├── pe-kind-control-plane
│   │   ├── root_netns_info.log
│   │   ├── router_info.log
│   │   └── router_netns_info.log
│   └── pe-kind-worker
│       ├── device_state
│       │   └── eth1.json
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

When OpenPERouter runs on systemd mode, node, router, Podman, systemd, and static-configuration information cannot be
collected through the cluster API because the router container is not managed by the cluster.

`inspect_host` can be used for collecting related info by executing the script directly on the target node.

Artifacts are stored at the host (default is `/openperouter-inspect-host`), and can be copied to base station for inspection.

### Prerequisites
- Direct shell access to the target node, for example through SSH or a kind node container.

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

### Environment variables
| Variable            | Description                                  | Default                    |
|---------------------|----------------------------------------------|----------------------------|
| `ARTIFACTS_DIR`     | Directory on the node for collected artifacts | `/openperouter-inspect-host` |
| `ROUTER_CONTAINER`  | OpenPERouter FRR container name              | `frr`                      |
| `CONF_DIR`          | Directory containing static YAML files       | `/var/lib/openperouter`    |

### Output
- `root_network/` - Root network namespace state, including links, addresses, IPv4 and IPv6 routes from all tables,
  neighbors, interface details and statistics, VRFs, and bridge FDB entries
- `router/router_info_podman.log` - OpenPERouter router and FRR information collected from the Podman FRR container
- `router/router_info_crictl.log` - OpenPERouter router and FRR information collected through crictl
- `podman/pods.log` and `podman/containers.log` - Podman pod and container state, including stopped containers
- `podman/logs/<container>.log` - Logs from the last ten minutes for available OpenPERouter Podman containers
- `systemd/<unit>.status.log` and `systemd/<unit>.journal.log` - Status and journal output for the hostmode
  OpenPERouter systemd units
- `configs/` - Static configuration resources in YAML form
- `config_files.log` - Static configuration collection log

When hostmode e2e specs fail, `inspect_host` runs on every node in the kind cluster. Its artifacts are copied into the
failed spec's inspect directory under `HOST_INFO/<node-name>/`, with one subdirectory per Kubernetes node.

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
│   ├── configs
│   │   └── node-config.yaml
│   ├── podman
│   │   ├── containers.log
│   │   ├── logs
│   │   │   ├── controller.log
│   │   │   ├── frr.log
│   │   │   └── reloader.log
│   │   └── pods.log
│   ├── root_network
│   │   ├── addresses.log
│   │   ├── bridge_fdb.log
│   │   ├── ipv4_routes_all_tables.log
│   │   ├── ipv6_routes_all_tables.log
│   │   ├── link_details_and_statistics.log
│   │   ├── links.log
│   │   ├── neighbors.log
│   │   └── vrfs.log
│   ├── router
│   │   └── router_info_podman.log
│   └── systemd
│       ├── controllerpod-pod.service.journal.log
│       ├── controllerpod-pod.service.status.log
│       ├── routerpod-pod.service.journal.log
│       └── routerpod-pod.service.status.log
└── pe-kind-worker
    └── ...
     
# inspect a specific node     
$ make inspect-systemd-mode NODES=pe-kind-worker
```
