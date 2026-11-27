# QEMU Containerlab Environment

This environment runs two Fedora Cloud VMs in Containerlab.
`pe-kind-control-plane` runs the k3s server and `pe-kind-worker` runs a k3s
agent. Both have the emulated NICs and VFs used by the QEMU end-to-end tests.
See [ARCHITECTURE.md](ARCHITECTURE.md) for the network layout.

## Deployment

```bash
make qemu-deploy
kubectl --kubeconfig bin/kubeconfig get nodes

# Run individual stages when debugging.
make qemu-image
make qemu-clab
make qemu-load-image IMG=quay.io/example/image:tag
make qemu-e2etests
```

`make qemu-clab` starts both guests, waits for both k3s nodes to be Ready,
and writes the kubeconfig to `bin/kubeconfig` by default. Set
`KUBECONFIG_PATH` to use another path. If a single-node QEMU lab is already
running, run `make qemu-clean` before deploying the two-node topology.

## Cleanup

```bash
make qemu-clean      # stop both VMs and remove the Containerlab topology
make qemu-destroy    # also remove the base image, overlays, logs, ISO, and SSH keys
```

## Troubleshooting

```bash
make qemu-ssh
make qemu-ssh-worker
sudo docker exec clab-kind-leafkind1 vtysh -c "show running-config"
```

The SSH ports default to 2222 for the control plane and 2223 for the worker.
Override them with `QEMU_SSH_PORT` and `QEMU_WORKER_SSH_PORT`. QEMU writes each
guest's console log to `vm/serial-<node-name>.log`.
