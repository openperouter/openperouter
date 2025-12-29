# Enhancement: Auto-Route for External BGP Neighbors

## Problem Statement

When BGP neighbors are configured outside the VTEP CIDR subnet, router pods cannot establish BGP sessions because they lack routing to reach those neighbors. This is particularly common in hybrid cloud scenarios where on-premises BGP peers need to be reached through VPN tunnels.

## Current Behavior

**Scenario:**
- VTEP CIDR: `10.0.200.0/24`
- BGP Neighbor: `10.250.1.3` (on-premises via VPN)

**Issue:**
Router pods have no route to `10.250.1.3`, causing BGP sessions to fail with "Connect" state.

**Current Workaround:**
Manual route addition in each router pod:
```bash
ip route add 10.250.1.0/24 dev net1 src 10.0.200.2
```

## Proposed Solution

Automatically add routes for BGP neighbors that are **outside the VTEP CIDR subnet**.

### Logic

```
For each neighbor in underlay.spec.neighbors:
  IF neighbor.address NOT IN vtepcidr:
    Add route: neighbor_subnet via vtep_interface src vtep_ip
```

### Implementation Location

**File:** `/home/ellorent/Documents/cnv/upstream/openperouter/internal/controller/routerconfiguration/host_config.go`

**After:** VTEP interface setup (around line 68-84 where VTEP IP is assigned)

### Pseudocode

```go
// After VTEP setup
if underlay.Spec.EVPN != nil {
    vtepCIDR, _ := netlink.ParseIPNet(underlay.Spec.EVPN.VTEPCIDR)
    vtepIP := getVTEPIP(vtepCIDR, nodeIndex)
    vtepInterface := getVTEPInterface() // e.g., "net1"

    for _, neighbor := range underlay.Spec.Neighbors {
        neighborIP := net.ParseIP(neighbor.Address)

        // Check if neighbor is NOT in VTEP CIDR
        if !vtepCIDR.Contains(neighborIP) {
            // Determine neighbor subnet (could be /32 or use a convention)
            neighborNet := getNeighborSubnet(neighborIP)

            route := &netlink.Route{
                LinkIndex: vtepInterface.Index,
                Dst:       neighborNet,
                Src:       vtepIP,
            }

            if err := netlink.RouteAdd(route); err != nil {
                // Handle error (route might already exist)
            }
        }
    }
}
```

### Subnet Determination

For the destination subnet, we have several options:

1. **Option A: /32 per neighbor** - Most conservative, one route per neighbor
   ```go
   neighborNet = &net.IPNet{IP: neighborIP, Mask: net.CIDRMask(32, 32)}
   ```

2. **Option B: Infer from neighbor address** - Assumes common subnet patterns
   ```go
   // If neighbor is 10.250.1.3, assume /24 network: 10.250.1.0/24
   neighborNet = inferSubnet(neighborIP)
   ```

3. **Option C: Add new CRD field** - Most explicit
   ```yaml
   neighbors:
   - address: 10.250.1.3
     asn: 64515
     network: 10.250.1.0/24  # New field
   ```

**Recommendation:** Start with **Option A** (/32 per neighbor) as it's the safest and doesn't require schema changes.

## Benefits

1. **Automatic Configuration** - No manual intervention needed
2. **Hybrid Cloud Ready** - Supports VPN/WAN scenarios out of the box
3. **Declarative** - Routes derived from Underlay spec
4. **Consistent** - Same behavior across all nodes

## Use Case: Hybrid Cloud with VPN

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay-worker-0
spec:
  asn: 65001
  evpn:
    vtepcidr: 10.0.200.0/24  # GCP VTEPs
  neighbors:
  - address: 10.0.200.1      # Worker-1 - IN vtepcidr → no route added
    asn: 65002
  - address: 10.0.200.3      # Worker-2 - IN vtepcidr → no route added
    asn: 65003
  - address: 10.250.1.3      # On-prem - OUT vtepcidr → route ADDED
    asn: 64515
    ebgpMultiHop: true
```

**Auto-generated route:**
```bash
ip route add 10.250.1.3/32 dev net1 src 10.0.200.2
```

## Testing

1. Deploy hybrid topology with on-premises BGP peer via VPN
2. Verify routes are automatically added in router pods
3. Verify BGP sessions establish without manual intervention
4. Test route cleanup when neighbor is removed

## Related Files

- `/home/ellorent/Documents/cnv/upstream/openperouter/internal/controller/routerconfiguration/host_config.go` - Route implementation
- `/home/ellorent/Documents/cnv/upstream/openperouter/internal/hostnetwork/underlay.go` - VTEP setup
- `/home/ellorent/Documents/cnv/upstream/openperouter/api/v1alpha1/underlay_types.go` - CRD definition (if adding network field)

## Date

2025-12-29

## Context

Discovered during hybrid GCP/on-premises setup with CloudVPN. Worker pods could ping on-premises leafgcp (10.250.1.3) via default route, but BGP failed because it needs to source from VTEP IP (10.0.200.x), requiring explicit route on the VTEP interface.
