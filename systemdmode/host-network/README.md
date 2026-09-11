# Host-network VNF proof of concept

Run the kernel router directly in a dedicated VM's host network namespace,
using static configuration and the existing systemd Quadlets. Build the router
image from this branch; released images do not have the `--router-netns` flag.

The controller compares the target namespace identity with its own host network
namespace. Paths and bind-mounted aliases referring to the host namespace select
the same behavior. The controller must run with host networking.

## Install on a fresh dedicated VM

Provision the VM's uplink addresses, routes and MTUs first. Adjust the example's
BGP neighbor, ASN and tunnel endpoint pool to match the fabric. The fabric must
provide reachability to the tunnel endpoint. Assign a unique node index per VM.

From the repository root, after making your locally built image available to
rootful Podman on the VM:

```bash
sudo mkdir -p /etc/containers/systemd /var/lib/openperouter/configs /run/netns
sudo cp systemdmode/quadlets/*.pod systemdmode/quadlets/*.container \
  systemdmode/quadlets/*.volume /etc/containers/systemd/
sudo cp -r systemdmode/host-network/routerpod.pod.d \
  systemdmode/host-network/controller.container.d /etc/containers/systemd/
sudo cp systemdmode/host-network/node-config.yaml /var/lib/openperouter/
sudo cp systemdmode/host-network/openpe_vnf.yaml /var/lib/openperouter/configs/
```

Set `Image=` in `controller.container`, `frr.container` and `reloader.container`
to your PoC image, then start the services:

```bash
sudo systemctl daemon-reload
sudo systemctl start routerpod-pod.service frr.service reloader.service
sudo systemctl start controllerpod-pod.service controller.service
```

The pod uses `/proc/1/ns/net` as seen by host-side Podman. The controller uses
`--router-netns=/hostproc/1/ns/net`, through its existing host `/proc` mount.
The drop-in clears the isolated namespace creation steps. No named namespace or
bind mount needs to be created. Without the drop-ins, the default remains
`/var/run/netns/perouter`.

For another pre-existing namespace, set the pod's `Network=ns:<host-path>` and
the controller's `--router-netns=<container-visible-path>` to the same namespace.
Custom namespaces are installation-owned; the controller does not create or
recover them. Do not change namespace mode on a running installation: migrate
or remove the previous dataplane state first.

## Behavior and PoC boundaries

- Uplinks are externally provisioned: they are not moved, marked with a link
  group, brought up/down or restored by OpenPERouter.
- `underlays[].interfaces` may be omitted for host networking. When supplied,
  only existing `NetworkDevice` entries are supported; names remain available
  for automatic IS-IS configuration. CNI uplink provisioning is unsupported.
- Automatic veth MTU adjustment is skipped in host networking. Configure the
  attachment MTUs externally to accommodate VXLAN/SRv6 overhead.
- OpenPERouter manages tunnel endpoint addresses on `lo` and overlay objects.
  Underlay removal cleans up overlay objects without moving NICs or clearing
  host loopback addresses. Removed/changed tunnel endpoint addresses must be
  cleaned up by VM provisioning for this PoC.
- The VM's VRFs are router-owned: reconciliation removes unconfigured VRFs.
- The example has no host BGP session. Same-namespace host sessions and coexistence
  with another host routing daemon require separate validation.
- Kubernetes is not required. With no kubeconfig the controller keeps its static
  reconciler running while waiting for API availability. Config files are watched.
- This PoC covers the kernel datapath and systemd installation, not grout.

## Verify

```bash
sudo stat -Lc '%d:%i' /proc/1/ns/net
sudo podman exec controller stat -Lc '%d:%i' /hostproc/1/ns/net
sudo podman exec frr stat -Lc '%d:%i' /proc/self/ns/net
sudo podman exec frr vtysh -c 'show bgp summary'
sudo podman exec frr vtysh -c 'show evpn vni'
ip -d link show
sudo journalctl -u controller.service -u frr.service -u reloader.service
```

The three namespace identities should match. Verify forwarding to a remote VTEP,
file updates, removal of the Underlay, individual container restarts and a VM
reboot. Uplink addresses, link groups and routes owned by VM provisioning should
remain intact across configuration removal; FRR-learned routes follow FRR's
lifecycle.
