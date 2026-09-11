# QEMU Containerlab Environment

This environment runs one Fedora Cloud VM inside the
`pe-kind-control-plane` Containerlab node. The VM hosts the single-node k3s
cluster used by the QEMU end-to-end tests. See
[ARCHITECTURE.md](ARCHITECTURE.md) for the complete network layout.

## Deployment

```bash
# Build the VM inputs, deploy containerlab, bootstrap k3s, load IMG, and deploy
# the controller.
make qemu-deploy

# Run individual stages when debugging.
make qemu-image
make qemu-clab
make qemu-load-image IMG=quay.io/example/image:tag
make qemu-e2etests
```

`make qemu-clab` starts QEMU and bootstraps k3s as part of the Containerlab
node setup. With the default root Makefile settings, it writes the guest
kubeconfig to `bin/kubeconfig` (or to `KUBECONFIG_PATH` when overridden).

## NIC Mapping

| Clab interface | TAP in QEMU container | Guest NIC    | MAC               |
|----------------|-----------------------|--------------|-------------------|
| toswitch1     | toswitch1_t     | toswitch1    | 52:54:00:ab:cd:01 |
| toswitch2     | toswitch2_t     | toswitch2    | 52:54:00:ab:cd:02 |
| toleafkind1   | toleafkind1_t   | toleafkind1  | 52:54:00:ab:cd:03 |
| toleafkind2   | toleafkind2_t   | toleafkind2  | 52:54:00:ab:cd:04 |

## Cleanup

```bash
make qemu-clean      # tear down VM + clab, preserve disk image
make qemu-destroy    # tear down clab and remove the base image, ISO, and SSH keys
```

The generated base image, cloud-init ISO, SSH keys, overlay disk, serial log,
and `clab-kind/` state are ignored by Git. `qemu-clean` removes the
Containerlab deployment but preserves the VM artifacts; `qemu-destroy` also
removes the base image, cloud-init ISO, and SSH keys. The overlay and serial
log may remain in `clab/qemu/vm` and can be removed separately if needed.

## Troubleshooting

```bash
sudo containerlab inspect --name kind
sudo docker exec clab-kind-pe-kind-control-plane ip link show
sudo docker exec clab-kind-leafkind1 vtysh -c "show running-config"
make qemu-ssh
make qemu-collect-logs
```
