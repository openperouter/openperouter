# Configuration Schema Contract: environment-config.yaml

**Format**: YAML

## Schema

```yaml
# IP address ranges for automatic allocation
ipRanges:
  pointToPoint:
    ipv4: "192.168.0.0/16"      # Base range for P2P IPv4 (/31 subnets)
    ipv6: "fd00::/48"            # Base range for P2P IPv6 (/127 subnets)
  broadcast:
    ipv4: "192.168.10.0/16"     # Base range for broadcast IPv4 (/24 subnets)
    ipv6: "fd00:10::/48"        # Base range for broadcast IPv6 (/64 subnets)
  vtep: "100.64.0.0/24"         # VTEP IP pool for edge-leaf nodes
  routerID: "10.0.0.0/24"       # BGP router ID pool

# Node configurations (applied via pattern matching)
nodes:
  - pattern: "leaf[AB]"          # Regex matching containerlab node names
    role: edge-leaf              # "edge-leaf" or "transit"
    evpnEnabled: true            # Optional, default: false
    vrfs:                        # Optional, edge-leaf only
      red:
        redistributeConnected: true
        interfaces:
          - ethred               # Must exist in clab topology links
        vni: 100
      blue:
        redistributeConnected: true
        interfaces:
          - ethblue
        vni: 200
    bgp:
      asn: 64520
      peers:
        - pattern: "spine"       # Regex matching peer node names
          evpnEnabled: true
          bfdEnabled: true

  - pattern: "spine"
    role: transit
    bgp:
      asn: 64612
      peers:
        - pattern: "leaf.*"
          evpnEnabled: true
          bfdEnabled: true

  - pattern: "leafkind"
    role: transit
    bgp:
      asn: 64512
      peers:
        - pattern: "spine"
          evpnEnabled: true
```

## Validation Rules

1. All `ipRanges` fields are required and must be valid CIDR notation
2. At least one entry in `nodes` is required
3. Each `pattern` must be a valid Go regexp
4. `role` must be either `edge-leaf` or `transit`
5. `vrfs` is only valid when `role` is `edge-leaf`
6. Each VRF must have at least one interface and a unique VNI
7. `bgp.asn` is required for every node config
8. `bgp.peers` must have at least one entry
9. No two node entries may have patterns that match the same containerlab node
