# PoC: NAD-backed underlay (controller-provisioned, capability IPAM)

Builds the router underlay interface from a Multus `NetworkAttachmentDefinition`
instead of moving a physical nic. The openperouter **controller** invokes the
NAD's CNI config (macvlan here) programmatically — via `containernetworking/cni`
libcni — directly into the persistent router netns (`/var/run/netns/perouter`),
and supplies a **per-node IP** (computed from `cidrs`) to the IPAM plugin via the
**CNI `ips` capability**.

No multus-dynamic-networks-controller, no pod annotation, no pod sandbox netns:
the controller is the CNI caller, so it targets the router netns directly
(`CNV-91179`).

## Files
- `nad.yaml` — the `NetworkAttachmentDefinition` (macvlan over `toswitch1`,
  `capabilities: {ips: true}` + `ipam: {type: static}`; no `addresses`).
- `underlay.yaml` — the `Underlay` referencing the NAD with a `cidrs` subnet.

## How the IP is assigned
openperouter passes the NAD config through unchanged and runs the plugin with the
CNI `ips` capability argument (`runtimeConfig.ips: [<per-node-cidr>]`). libcni
injects it because the plugin declares `capabilities.ips=true`; the static IPAM
consumes it. So the per-node IP is pure runtime data — the NAD has no addresses.

## Requirements (already met by the dev env)
- `macvlan` + `static` CNI plugins on each node, **statically linked**
  (`clab/kind/frr-k8s/setup.sh` builds them with `CGO_ENABLED=0` so the Alpine
  controller can exec them).
- The `NetworkAttachmentDefinition` CRD (installed with Multus in the dev env).
- The controller pod mounts `/opt/cni/bin` (ro) + `/var/lib/cni` (rw).

## Run
```bash
make deploy                                   # kind + containerlab dev env
kubectl apply -f examples/evpn/nad-underlay/  # nad.yaml + underlay.yaml
```

## Verify
```bash
# The underlay interface created by CNI, inside the persistent router netns,
# with the openperouter-assigned IP (node index 0 -> 192.168.11.10):
kubectl -n openperouter-system exec ds/controller -- \
  ip netns exec perouter ip -br addr show underlay0
# -> underlay0  UP  192.168.11.10/24 ...

# BGP session to the leaf (192.168.11.2):
kubectl -n openperouter-system exec ds/router -c frr -- vtysh -c "show bgp summary"
```

## Notes
- **Addressing:** the cidr's address part is the allocation start, so node index
  `i` gets `start+i` (e.g. `192.168.11.10/24` -> `.10`, `.11`, ...). The start
  (`.10`) avoids the dev-env addresses on this segment (leaf `.2`, node uplinks
  `.3`/`.4`), so every node gets a collision-free, on-link address regardless of
  index — no nodeSelector needed.
- **Lifecycle:** deleting the `Underlay` runs CNI DEL (via the libcni result
  cache) and removes `underlay0`; the macvlan parent `toswitch1` stays on the
  host. The interface also survives router-pod restarts (it lives in the
  controller-owned persistent netns).
- To extend into a full EVPN demo, add `L3VNI`/`L2VNI` resources as in
  `examples/evpn/metallb/openpe.yaml`.
