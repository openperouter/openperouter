# PoC: NAD-backed underlay with DHCP IPAM (controller-supervised dhcp daemon)

Builds the router underlay interface from a Multus `NetworkAttachmentDefinition`
whose IPAM is **`dhcp`**, so the per-node address is **leased** rather than
computed from `cidrs`. As in the [`nad-underlay`](../nad-underlay/) example the
openperouter **controller** invokes the NAD's CNI config (macvlan) programmatically
into the persistent router netns (`/var/run/netns/perouter`); the difference is
the address source.

## How the IP is assigned
The `Underlay` has **no `cidrs`**, so openperouter passes the NAD through
unchanged and does **not** inject the CNI `ips` capability. The macvlan plugin
delegates to its `ipam: {type: dhcp}`, which talks to the CNI **dhcp daemon**.
The controller supervises that daemon as a child process (`--dhcp-bin`), so no
separate DaemonSet is needed: the daemon shares the controller's mount namespace
and privileges, reaches the router netns directly, and listens on the in-container
default socket `/run/cni/dhcp.sock` that the plugin connects to.

## Lease survival across restarts
The dhcp daemon keeps leases **in memory**. A controller/daemon restart would
otherwise forget the lease and stop renewing it (the interface lives in the
persistent router netns and outlives the controller), so the address would lapse
at lease expiry. This is handled:

- The supervisor signals a channel after every (re)start of the daemon.
- The reconciler watches that channel and, for an already-existing DHCP underlay,
  re-invokes the dhcp IPAM directly (`cni.EnsureIPAM`). The daemon's `Allocate`
  is idempotent by client-ID (`containerID/name/ifName`, all stable here), so it
  re-acquires the same lease and **resumes renewal**.
- If the lease was re-acquired so late that the server already reassigned the IP,
  the interface address is reconciled to the new lease (`AddrReplace`).

## Requirements (met by the dev env)
- `macvlan` + `dhcp` CNI plugins on each node, **statically linked**
  (`clab/kind/frr-k8s/setup.sh` builds them with `CGO_ENABLED=0`).
- The controller runs with `--dhcp-bin=/opt/cni/bin/dhcp` (set in
  `config/pods/controller.yaml`; Helm: `openperouter.dhcpBin`).
- A DHCP server on the underlay segment. The dev env adds a dnsmasq clab node
  (`dhcp1`) on `leafkind1-sw` serving `192.168.11.100-150` with a short 2m lease.

## Files
- `nad.yaml` — the `NetworkAttachmentDefinition` (macvlan over `toswitch1`,
  `ipam: {type: dhcp}`; no capabilities).
- `underlay.yaml` — the `Underlay` referencing the NAD with **no `cidrs`**.

## Run
```bash
make deploy                                        # kind + containerlab dev env
kubectl apply -f examples/evpn/nad-underlay-dhcp/  # nad.yaml + underlay.yaml
```

## Verify
```bash
# Underlay interface created by CNI, inside the persistent router netns, with a
# DHCP-leased address from the dnsmasq pool (192.168.11.100-150):
kubectl -n openperouter-system exec ds/controller -- \
  ip netns exec perouter ip -br addr show underlay0
# -> underlay0  UP  192.168.11.1xx/24 ...

# BGP session to the leaf (192.168.11.2):
kubectl -n openperouter-system exec ds/router -c frr -- vtysh -c "show bgp summary"

# dnsmasq lease activity:
docker logs clab-kind-dhcp1 2>&1 | grep DHCP | tail
```

## Test restart resilience
```bash
# Kill the dhcp daemon subprocess; the supervisor restarts it and the reconciler
# re-acquires the SAME lease. BGP must stay up across several 2m lease periods.
kubectl -n openperouter-system exec ds/controller -- pkill -f 'dhcp daemon'
kubectl -n openperouter-system logs ds/controller | grep -i 'dhcp daemon ready'

# Restart the controller pod; underlay0 persists, the lease is re-acquired.
kubectl -n openperouter-system rollout restart ds/controller
```

## Notes
- **Lifecycle:** deleting the `Underlay` runs CNI DEL (via the libcni result
  cache) and removes `underlay0`. The upstream dhcp daemon does not reliably emit
  a `DHCPRELEASE` when driven this way, so the freed lease is reclaimed only when
  it expires — keep lease times modest. The per-node client-id means the same
  node always re-leases the same address, so there is no cross-node conflict.
- **Per-node client-id:** the CNI container ID (hence the DHCP client identifier,
  option 61) is node-scoped (`perouter-underlay-<node>`). Without this every node
  presents the shared server the same client-id and they collide onto one lease.
- **Known gap (not fixed here):** a lease can still lapse if the daemon stays
  down longer than the lease time (e.g. the controller is unschedulable). The
  re-acquire path covers normal restarts, not indefinite outages.
- Mixing addressing modes is per-Underlay: set `cidrs` to have openperouter
  assign the IP (see [`nad-underlay`](../nad-underlay/)), or omit them to lease
  via the NAD's IPAM as here.
