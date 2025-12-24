# Hybrid Topology - On-Prem + GCP via CloudVPN

This topology connects an on-premises kind cluster running openperouter with GCP OpenShift workers via CloudVPN and BGP EVPN.

## Architecture

```
On-Prem (Containerlab)                    GCP
┌────────────────────────┐               ┌─────────────────────┐
│  Spine (64612)         │               │  OpenShift Workers  │
│  10.250.1.0/31         │               │  10.0.200.1-3       │
│  10.250.1.2/31         │               │  ASN 65001-65003    │
│  (Route Reflector)     │               └──────────┬──────────┘
└───┬────────────┬───────┘                          │
    │            │                                  │
    │            │                          [CloudVPN Tunnel]
    │            │                                  │
┌───┴────┐   ┌───┴──────────┐                      │
│leafkind│   │   leafgcp    │◄─────────────────────┘
│64512   │   │   64515      │ strongSwan VPN Client
│        │   │              │ 10.250.1.3/31
└───┬────┘   └──────────────┘
    │
┌───┴─────┐
│ Switch  │
│10.250.11│
└─┬─────┬─┘
  │     │
┌─┴─┐ ┌─┴──┐
│Ctrl│ │Work│
│.3  │ │.4  │
└────┘ └────┘
openperouter
ASN 64514
```

## IP Addressing

### Underlay (Spine-Leaf)
- Spine: 10.250.1.0/31, 10.250.1.2/31
- leafkind: 10.250.1.1/31
- leafgcp: 10.250.1.3/31

### Kind Cluster
- leafkind bridge: 10.250.11.1/24
- Control plane: 10.250.11.3/24
- Worker: 10.250.11.4/24

### GCP (via VPN)
- Workers: 10.0.200.1, 10.0.200.2, 10.0.200.3

## BGP Topology

### ASN Assignment
- **64612**: Spine (Route Reflector)
- **64512**: leafkind
- **64514**: Kind cluster openperouter routers
- **64515**: leafgcp
- **65001-65003**: GCP workers

### BGP Sessions
1. Spine ↔ leafkind (64612 ↔ 64512)
2. Spine ↔ leafgcp (64612 ↔ 64515)
3. leafkind ↔ kind nodes (64512 ↔ 64514, dynamic)
4. leafgcp ↔ GCP workers (64515 ↔ 65001-65003, dynamic over VPN)

## VPN Configuration

### GCP Side
- Gateway IP: 34.16.52.245
- Local Traffic Selector: 10.0.200.0/24
- Remote Traffic Selector: 10.250.1.0/24

### On-Prem (leafgcp)
- Local IP: 79.117.12.111
- Remote IP: 34.16.52.245
- Shared Secret: RdRTEpoYc/2E44SSIv3bfwGsN0PkNuBP
- Traffic Selectors:
  - Local: 10.250.1.0/24
  - Remote: 10.0.200.0/24

## Deployment

### Prerequisites

1. Build the FRR+VPN Docker image:
```bash
cd /home/ellorent/Documents/cnv/upstream/openperouter/clab/dockerfile
docker build -f Dockerfile.frr-vpn -t frr-vpn:latest .
```

2. Ensure GCP CloudVPN is configured:
```bash
cd /home/ellorent/Documents/cnv/sandbox/gcp
./resources/setup-gcp-cloudvpn.sh
```

### Deploy Topology

```bash
cd /home/ellorent/Documents/cnv/upstream/openperouter/clab

# Set environment variables
export CLAB_TOPOLOGY="hybrid/kind.clab.yml"
export CLUSTER_NAMES="pe-kind"

# Deploy
./setup.sh pe-kind
```

Or manually:
```bash
cd /home/ellorent/Documents/cnv/upstream/openperouter/clab
sudo clab deploy --reconfigure --topo hybrid/kind.clab.yml
```

### Verification

1. Check VPN status:
```bash
docker exec clab-kind-leafgcp swanctl --list-sas
```

2. Check BGP sessions:
```bash
# Spine
docker exec clab-kind-spine vtysh -c "show bgp summary"

# leafgcp
docker exec clab-kind-leafgcp vtysh -c "show bgp summary"

# leafkind
docker exec clab-kind-leafkind vtysh -c "show bgp summary"
```

3. Check EVPN routes:
```bash
docker exec clab-kind-leafgcp vtysh -c "show bgp l2vpn evpn"
```

## L2VNI Configuration

To stretch L2VNI between on-prem and GCP:

1. Apply same L2VNI on both clusters:
```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: L2VNI
metadata:
  name: east-west
  namespace: openperouter-system
spec:
  vni: 1000
  vrf: east-west
  vxlanport: 4789
  l2gatewayips:
  - 192.168.100.1/24
```

2. Pods in same VNI can communicate across clouds using VXLAN over VPN

## Troubleshooting

### VPN Not Establishing
```bash
# Check leafgcp logs
docker logs clab-kind-leafgcp

# Check IPsec status
docker exec clab-kind-leafgcp swanctl --list-sas
docker exec clab-kind-leafgcp ipsec status
```

### BGP Sessions Not Establishing
```bash
# Check reachability
docker exec clab-kind-leafgcp ping -c 3 10.250.1.2  # spine
docker exec clab-kind-leafgcp ping -c 3 10.0.200.1  # GCP worker

# Check FRR logs
docker exec clab-kind-leafgcp tail -f /var/log/frr/frr.log
```

### VXLAN Not Working
```bash
# Check if EVPN routes are being exchanged
docker exec clab-kind-leafgcp vtysh -c "show bgp l2vpn evpn"

# Check VTEP IPs
kubectl get pods -n openperouter-system -o wide
```
