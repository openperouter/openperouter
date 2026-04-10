# Underlay CRD Contract

**Feature**: 006-multi-underlay-neighbors  
**API Version**: openpe.openperouter.github.io/v1alpha1  
**Kind**: Underlay  
**Scope**: Namespaced

## Schema Definition

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: underlays.openpe.openperouter.github.io
spec:
  group: openpe.openperouter.github.io
  names:
    kind: Underlay
    listKind: UnderlayList
    plural: underlays
    singular: underlay
  scope: Namespaced
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        properties:
          spec:
            properties:
              nodeSelector:
                description: Nodes this Underlay applies to (optional, default: all nodes)
                type: object
                x-kubernetes-preserve-unknown-fields: true
                
              asn:
                description: Local BGP AS number
                type: integer
                minimum: 1
                maximum: 4294967295
                
              routerIDCIDR:
                description: IPv4 CIDR for router ID assignment per node
                type: string
                default: "10.0.0.0/24"
                
              neighbors:
                description: List of BGP neighbors (minimum 1 required)
                type: array
                minItems: 1
                items:
                  type: object
                  required:
                  - asn
                  - address
                  properties:
                    asn:
                      description: Remote AS number
                      type: integer
                      minimum: 1
                      maximum: 4294967295
                    hostASN:
                      description: AS number for host namespace BGP component
                      type: integer
                      minimum: 0
                      maximum: 4294967295
                    address:
                      description: IP address of neighbor
                      type: string
                    port:
                      description: BGP port (default 179)
                      type: integer
                      minimum: 0
                      maximum: 16384
                    password:
                      description: BGP authentication password
                      type: string
                    bfd:
                      description: BFD configuration
                      type: object
                      properties:
                        enabled:
                          type: boolean
                        minRx:
                          type: integer
                        minTx:
                          type: integer
                        multiplier:
                          type: integer
                    ebgpMultihop:
                      description: eBGP multihop TTL
                      type: integer
                      
              nics:
                description: Physical interface names to move to router namespace
                type: array
                items:
                  type: string
                  pattern: '^[a-zA-Z][a-zA-Z0-9._-]*$'
                  maxLength: 15
                  
              evpn:
                description: EVPN-VXLAN configuration
                type: object
                x-kubernetes-validations:
                - rule: "(has(self.vtepcidr) && self.vtepcidr != '') != (has(self.vtepInterface) && self.vtepInterface != '')"
                  message: "exactly one of vtepcidr or vtepInterface must be specified"
                properties:
                  vtepcidr:
                    description: CIDR for VTEP IP allocation
                    type: string
                  vtepInterface:
                    description: Existing interface name for VTEP source
                    type: string
                    pattern: '^[a-zA-Z][a-zA-Z0-9._-]*$'
                    maxLength: 15
```

## Validation Rules

### CRD-Level (Kubernetes API Server)

1. **Required Fields**:
   - `spec.asn` - Must be present
   - `spec.neighbors[].asn` - Each neighbor must have ASN
   - `spec.neighbors[].address` - Each neighbor must have address

2. **Type Validation**:
   - ASN: uint32, range 1-4294967295
   - Port: uint16, range 0-16384
   - Interface names: Pattern `^[a-zA-Z][a-zA-Z0-9._-]*$`, max 15 chars

3. **Array Constraints**:
   - `neighbors`: Minimum 1 element required (`minItems: 1`)
   - `nics`: Optional, no minimum (can be empty if using Multus)

4. **XOR Constraint**:
   - EVPN: Exactly one of `vtepcidr` or `vtepInterface` must be set

### Webhook-Level (Custom Validation)

Enforced by admission webhook at `/validate-openperouter-io-v1alpha1-underlay`:

1. **Uniqueness**:
   - Neighbor addresses must be unique within `spec.neighbors` array
   - Nic names must be unique within `spec.nics` array
   - No overlapping node selectors across Underlays

2. **Cross-Field**:
   - Local ASN must differ from all neighbor ASNs
   - If EVPN is nil and L3VNIs/L2VNIs exist → Reject
   - At least one neighbor OR one nic must be specified

3. **Format**:
   - VTEP CIDR must be valid CIDR notation
   - Neighbor addresses must be valid IPv4 or IPv6
   - Router ID CIDR must be valid CIDR notation

## Breaking Changes

### From Current Version

**None** - This is a backward-compatible extension:

- Arrays already exist in schema (`neighbors`, `nics`)
- Only removing artificial validation limits
- Single-element arrays remain valid

### Migration Path

**No migration required**:

1. Existing single-neighbor configs continue to work
2. Existing single-nic configs continue to work
3. Users can update specs to add more neighbors/nics at any time

## Error Responses

### Validation Errors

```json
{
  "kind": "Status",
  "apiVersion": "v1",
  "status": "Failure",
  "message": "admission webhook \"underlayvalidationwebhook.openperouter.io\" denied the request: validation failed: duplicate neighbor address 192.168.1.1",
  "reason": "Invalid",
  "code": 400
}
```

### Common Errors

1. **Duplicate Neighbor**: 
   ```
   "validation failed: duplicate neighbor address <IP>"
   ```

2. **Duplicate NIC**: 
   ```
   "validation failed: duplicate nic name <interface>"
   ```

3. **ASN Conflict**: 
   ```
   "underlay <name> local ASN <N> must be different from remote ASN <N>"
   ```

4. **Node Selector Overlap**: 
   ```
   "node <name> matched by multiple underlays"
   ```

5. **Invalid CIDR**: 
   ```
   "invalid vtep CIDR format for underlay <name>: <cidr>"
   ```

## Resource Limits

No hard-coded limits on arrays:

- **Neighbors**: Limited by available system resources
  - Practical testing: Up to 20 neighbors per underlay
  - FRR supports hundreds of BGP sessions
  
- **NICs**: Limited by available network interfaces
  - Practical testing: Up to 10 interfaces per underlay
  - Linux supports thousands of interfaces

Performance impact scales linearly with number of entities.
