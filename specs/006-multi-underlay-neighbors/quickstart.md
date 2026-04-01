# Quickstart: Multi-Interface Multi-Neighbor Development

**Feature**: 006-multi-underlay-neighbors  
**Target Audience**: Developers implementing or testing this feature  
**Prerequisites**: Linux development environment, Go 1.25+, containerlab 0.74.1+

## Development Environment Setup

### 1. Clone and Build

```bash
# Navigate to repository
cd /path/to/openperouter

# Checkout feature branch
git checkout 006-multi-underlay-neighbors

# Install dependencies
go mod download

# Build operator
make build

# Generate CRD manifests
make manifests

# Run unit tests
make test
```

### 2. Install Containerlab (for E2E tests)

```bash
# Install containerlab 0.74.1 or later
bash -c "$(curl -sL https://get.containerlab.dev)"

# Verify installation
containerlab version  # Should be >= 0.74.1
```

### 3. Set Up Test Environment

```bash
# Start a Kind cluster for E2E testing
make kind-cluster

# Install CRDs
make install

# Deploy operator
make deploy
```

## Quick Test Scenarios

### Scenario 1: Validate Multi-Neighbor Configuration

**Test Goal**: Verify webhook accepts multiple neighbors

```bash
# Create test configuration
cat <<EOF | kubectl apply -f -
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: test-multi-neighbor
  namespace: default
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  - asn: 65002
    address: "192.168.1.2"
  - asn: 65003
    address: "192.168.2.1"
  nics:
  - "eth0"
  evpn:
    vtepcidr: "10.100.0.0/24"
EOF

# Verify it was accepted
kubectl get underlay test-multi-neighbor -o yaml

# Should show all 3 neighbors in spec
```

**Expected Result**: Resource created successfully with 3 neighbors

### Scenario 2: Validate Multi-Interface Configuration

**Test Goal**: Verify webhook accepts multiple interfaces

```bash
cat <<EOF | kubectl apply -f -
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: test-multi-nic
  namespace: default
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  nics:
  - "eth1"
  - "eth2"
  - "eth3"
  evpn:
    vtepcidr: "10.100.0.0/24"
EOF

kubectl get underlay test-multi-nic -o yaml
# Should show all 3 nics in spec
```

**Expected Result**: Resource created successfully with 3 interfaces

### Scenario 3: Test Validation - Duplicate Neighbor

**Test Goal**: Verify webhook rejects duplicate neighbor addresses

```bash
cat <<EOF | kubectl apply -f -
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: test-invalid-duplicate
  namespace: default
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  - asn: 65003
    address: "192.168.1.1"  # DUPLICATE!
  nics:
  - "eth0"
  evpn:
    vtepcidr: "10.100.0.0/24"
EOF
```

**Expected Result**: Admission denied with error message about duplicate neighbor address

### Scenario 4: Test Backward Compatibility

**Test Goal**: Ensure existing single-interface/neighbor configs still work

```bash
# Deploy old-style single-entity config
cat <<EOF | kubectl apply -f -
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: test-backward-compat
  namespace: default
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  nics:
  - "eth0"
  evpn:
    vtepcidr: "10.100.0.0/24"
EOF

# Verify acceptance
kubectl get underlay test-backward-compat
```

**Expected Result**: Resource created successfully (backward compatible)

## E2E Testing with Containerlab

### Setup Containerlab Topology

Create a multi-interface test topology:

```bash
# Create topology file
cat > test-topology.clab.yml <<EOF
name: multi-underlay-e2e
topology:
  nodes:
    router:
      kind: linux
      image: ghcr.io/openperouter/openperouter:dev
      
    tor1:
      kind: linux
      image: frrouting/frr:latest
      
    tor2:
      kind: linux
      image: frrouting/frr:latest
      
    tor3:
      kind: linux
      image: frrouting/frr:latest
      
  links:
    # Multiple links from router to different TORs
    - endpoints: ["router:eth1", "tor1:eth1"]
    - endpoints: ["router:eth2", "tor2:eth1"]
    - endpoints: ["router:eth3", "tor3:eth1"]
    - endpoints: ["router:eth4", "tor1:eth2"]  # Redundant path to TOR1
EOF

# Deploy topology
sudo containerlab deploy -t test-topology.clab.yml

# Verify topology
sudo containerlab inspect -t test-topology.clab.yml
```

### Run E2E Test Suite

```bash
# Run full E2E test suite
make e2e-test

# Run specific test for multi-neighbor
go test ./e2etests/tests -v -run TestMultiNeighbor

# Run specific test for multi-interface
go test ./e2etests/tests -v -run TestMultiInterface

# Run combined multi-entity test
go test ./e2etests/tests -v -run TestMultiUnderlayFull
```

### Manual E2E Verification

```bash
# Deploy Underlay with multiple interfaces/neighbors
kubectl apply -f - <<EOF
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: e2e-multi-test
  namespace: default
spec:
  asn: 65001
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  - asn: 65003
    address: "192.168.2.1"
  - asn: 65004
    address: "192.168.3.1"
  nics:
  - "eth1"
  - "eth2"
  - "eth3"
  evpn:
    vtepcidr: "10.100.0.0/24"
EOF

# Wait for reconciliation
kubectl wait --for=condition=Ready underlay/e2e-multi-test --timeout=60s

# Check FRR BGP status
kubectl exec -it deployment/openperouter-router -- vtysh -c "show bgp summary"
# Should show 3 neighbors

# Check interfaces in namespace
kubectl exec -it deployment/openperouter-router -- ip link show
# Should show eth1, eth2, eth3 in router namespace

# Verify BGP sessions established
kubectl exec -it deployment/openperouter-router -- vtysh -c "show bgp neighbors" | grep "BGP state"
# Should show "Established" for all 3 neighbors

# Test data plane connectivity
kubectl exec -it deployment/openperouter-router -- ping -c 3 192.168.1.1
kubectl exec -it deployment/openperouter-router -- ping -c 3 192.168.2.1
kubectl exec -it deployment/openperouter-router -- ping -c 3 192.168.3.1
```

## Unit Testing Key Components

### Test Validation Logic

```bash
# Test underlay validation
go test ./internal/conversion -v -run TestValidateUnderlay

# Should test:
# - Single underlay (backward compat)
# - Multiple neighbors
# - Multiple nics
# - Duplicate neighbor rejection
# - Duplicate nic rejection
# - ASN conflict detection
```

### Test Conversion Logic

```bash
# Test API to FRR conversion
go test ./internal/conversion -v -run TestAPItoFRR

# Should verify:
# - All neighbors converted to FRR config
# - Multiple interfaces handled
# - BFD profiles generated for each neighbor
```

### Test Webhook

```bash
# Test webhook validation
go test ./internal/webhooks -v -run TestUnderlayWebhook

# Should cover:
# - Accept valid multi-entity configs
# - Reject duplicate neighbors
# - Reject duplicate nics
# - Reject ASN conflicts
# - Accept backward-compatible single-entity
```

## Debugging Tips

### 1. Enable Debug Logging

```bash
# Set log level in operator deployment
kubectl set env deployment/openperouter-controller-manager -n openperouter-system LOG_LEVEL=debug

# Watch logs
kubectl logs -f deployment/openperouter-controller-manager -n openperouter-system
```

### 2. Check Validation Webhook

```bash
# View webhook configuration
kubectl get validatingwebhookconfigurations

# Describe webhook
kubectl describe validatingwebhookconfiguration openperouter-validating-webhook-configuration

# Check webhook pod logs
kubectl logs -n openperouter-system deployment/openperouter-webhook -f
```

### 3. Inspect FRR Configuration

```bash
# View generated FRR config
kubectl exec -it deployment/openperouter-router -- cat /etc/frr/frr.conf

# Should see multiple "neighbor" statements
# Example:
#   neighbor 192.168.1.1 remote-as 65002
#   neighbor 192.168.2.1 remote-as 65003
#   neighbor 192.168.3.1 remote-as 65004
```

### 4. Check Interface Movement

```bash
# Check interfaces in host namespace
ip link show

# Check interfaces in router namespace
kubectl exec -it deployment/openperouter-router -- ip link show

# Interfaces should have moved from host to router namespace
```

### 5. Verify Hot-Apply vs Restart

```bash
# Check router container uptime before update
kubectl exec -it deployment/openperouter-router -- uptime

# Apply config update (add neighbor)
kubectl patch underlay e2e-multi-test --type merge -p '
spec:
  neighbors:
  - asn: 65002
    address: "192.168.1.1"
  - asn: 65003
    address: "192.168.2.1"
  - asn: 65004
    address: "192.168.3.1"
  - asn: 65005
    address: "192.168.4.1"
'

# Check uptime again
kubectl exec -it deployment/openperouter-router -- uptime
# Uptime should NOT have reset (hot-apply worked)

# Verify new neighbor added
kubectl exec -it deployment/openperouter-router -- vtysh -c "show bgp summary"
# Should now show 4 neighbors
```

## Common Issues and Solutions

### Issue 1: Webhook Denies Valid Config

**Symptom**: Webhook rejects multi-entity config even though it looks valid

**Debug**:
```bash
# Check webhook logs
kubectl logs -n openperouter-system deployment/openperouter-webhook

# Look for specific validation error
```

**Solution**: Ensure uniqueness constraints met (no duplicate addresses/nics)

### Issue 2: FRR Config Not Generated Correctly

**Symptom**: FRR config shows only one neighbor despite multiple in spec

**Debug**:
```bash
# Check conversion logic
kubectl logs -f deployment/openperouter-controller-manager | grep "converting underlay"

# Inspect generated config
kubectl exec deployment/openperouter-router -- cat /etc/frr/frr.conf
```

**Solution**: Check `frr_conversion.go` neighbor iteration loop

### Issue 3: Interfaces Not Moved to Namespace

**Symptom**: Interfaces remain in host namespace

**Debug**:
```bash
# Check host conversion logs
kubectl logs -f deployment/openperouter-controller-manager | grep "moving interface"

# Verify interface exists on host
ip link show eth1
```

**Solution**: Check `host_conversion.go` interface iteration, verify interface names correct

### Issue 4: BGP Sessions Not Establishing

**Symptom**: BGP neighbors show "Idle" or "Connect" state

**Debug**:
```bash
# Check FRR logs
kubectl exec deployment/openperouter-router -- cat /var/log/frr/bgpd.log

# Check connectivity
kubectl exec deployment/openperouter-router -- ping 192.168.1.1
```

**Solution**: Verify network connectivity, check firewall rules, verify neighbor IPs

## Performance Testing

### Test with Maximum Neighbors/Interfaces

```bash
# Create config with maximum practical entities
cat > max-entities.yaml <<EOF
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: test-max-entities
  namespace: default
spec:
  asn: 65001
  neighbors:
  # Add 20 neighbors (practical maximum for testing)
  $(for i in {1..20}; do
    echo "  - asn: $((65000 + i))"
    echo "    address: \"192.168.$((i/250)).$((i%250))\""
  done)
  nics:
  # Add 10 interfaces (practical maximum for testing)
  $(for i in {1..10}; do
    echo "  - \"eth$i\""
  done)
  evpn:
    vtepcidr: "10.100.0.0/24"
EOF

# Apply and measure reconciliation time
time kubectl apply -f max-entities.yaml

# Should complete in < 2 seconds (per success criteria)
```

### Benchmark Validation Performance

```bash
# Run validation benchmark
go test ./internal/webhooks -bench=BenchmarkValidateUnderlay -benchmem

# Should show reasonable performance even with max entities
```

## Next Steps

After validating basic functionality:

1. **Run Full E2E Suite**: `make e2e-test`
2. **Test Hot-Apply**: Verify config updates don't trigger restarts
3. **Test Backward Compat**: Ensure single-entity configs work
4. **Performance Test**: Validate with maximum practical entities
5. **Integration Test**: Test with FRR-K8s, MetalLB, or Calico

## Resources

- **Spec**: `specs/006-multi-underlay-neighbors/spec.md`
- **Research**: `specs/006-multi-underlay-neighbors/research.md`
- **Data Model**: `specs/006-multi-underlay-neighbors/data-model.md`
- **API Contracts**: `specs/006-multi-underlay-neighbors/contracts/`
- **E2E Tests**: `e2etests/tests/`
- **Containerlab Docs**: https://containerlab.dev/

## Getting Help

- **Code Issues**: Check existing code at `internal/conversion/`, `internal/webhooks/`
- **Test Failures**: Review test output and logs carefully
- **Topology Issues**: Verify containerlab topology with `sudo containerlab inspect`
- **BGP Issues**: Check FRR logs and configuration
