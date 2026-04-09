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

The E2E test suite validates multi-interface and multi-neighbor functionality using containerlab to create a realistic network topology with multiple leaf switches, ToR switches, and a Kind Kubernetes cluster.

### Test Strategy Overview

The test suite uses a dual-leaf topology that mirrors production datacenter deployments:

**Topology Architecture**:
- **2 Leaf Nodes** (leafA, leafB): Simulate external hosts/networks
- **2 ToR Switches** (leafkind, leafkind2): Provide BGP underlay connectivity
- **Kind Cluster**: Runs OpenPERouter and test workloads
- **Spine Switch**: Provides leaf-to-leaf connectivity
- **Test Hosts**: Validate L3 connectivity across VRFs

**Test Categories**:

1. **Single-Session Tests** (baseline validation):
   - Purpose: Validate basic functionality with minimal complexity
   - Config: 1 interface, 1 neighbor
   - Validates: BGP session establishment, basic L3 connectivity
   
2. **Multi-Session Tests** (full multi-entity validation):
   - Purpose: Validate production-like configurations
   - Config: 2 interfaces, 4 neighbors (matches production topology)
   - Validates: All BGP sessions establish, L3 connectivity from both leafs, multi-path routing

### Understanding the Containerlab Topology

The actual E2E topology is defined in `/home/fpaoline/openperouter1/clab/singlecluster/kind.clab.yml`:

```yaml
topology:
  nodes:
    # Leaf switches - simulate external hosts/DC fabric
    leafA: FRR router (AS varies per test)
    leafB: FRR router (AS varies per test)
    
    # ToR switches - BGP neighbors for OpenPERouter
    leafkind: FRR router, peers with kind cluster via toswitch
    leafkind2: FRR router, peers with kind cluster via toswitch2
    
    # Spine - connects all leaves together
    spine: FRR router, provides leaf-to-leaf connectivity
    
    # Kind cluster - runs OpenPERouter
    pe-kind: Kind cluster (control-plane + worker nodes)
    
    # Test hosts - validate L3 connectivity
    hostA_default, hostA_red, hostA_blue: Connected to leafA
    hostB_red, hostB_blue: Connected to leafB

  links:
    # Underlay connectivity (Kind <-> ToRs)
    - pe-kind nodes connect to both leafkind and leafkind2 via bridges
    - Each kind node has toswitch (-> leafkind) and toswitch2 (-> leafkind2)
    
    # Fabric connectivity (ToRs <-> Spine <-> External Leafs)
    - All leaf switches connect to spine
    - Creates full-mesh reachability
```

**Key Topology Features**:
- **Dual-homing**: Kind nodes connect to BOTH ToR switches for redundancy
- **Multi-path**: Traffic can flow through either leafkind or leafkind2
- **Full L3 validation**: External hosts can reach pods via EVPN/VXLAN

### Running E2E Tests Locally

#### Prerequisites

```bash
# Install containerlab 0.74.1+
bash -c "$(curl -sL https://get.containerlab.dev)"
containerlab version  # Should be >= 0.74.1

# Ensure Docker is running
docker ps

# Install Kind
GO111MODULE=on go install sigs.k8s.io/kind@latest
```

#### Deploy Test Environment

```bash
# Navigate to project root
cd /home/fpaoline/openperouter1

# Deploy containerlab topology (creates Kind cluster + network fabric)
cd clab/singlecluster
sudo containerlab deploy -t kind.clab.yml

# Wait for topology to be ready (can take 2-3 minutes)
sudo containerlab inspect -t kind.clab.yml

# Verify Kind cluster is accessible
export KUBECONFIG=~/.kube/config
kubectl config use-context kind-kind
kubectl get nodes
# Should show: pe-kind-control-plane and pe-kind-worker
```

#### Run E2E Test Suite

```bash
# Return to project root
cd /home/fpaoline/openperouter1

# Run full E2E suite
make e2e-test

# Or run specific test suites:

# Test multi-session BGP establishment (4 neighbors)
go test ./e2etests/tests -v -run TestSessions -timeout 10m

# Test webhook validation (duplicate detection, ASN conflicts)
go test ./e2etests/tests -v -run TestWebhooks -timeout 5m

# Test L3 connectivity from external hosts
go test ./e2etests/tests -v -run TestL3Connectivity -timeout 10m
```

#### Understanding Test Configurations

The tests use the following Underlay configuration (from `e2etests/pkg/infra/underlay.go`):

```yaml
apiVersion: openpe.openperouter.github.io/v1alpha1
kind: Underlay
metadata:
  name: underlay
  namespace: openperouter-system
spec:
  asn: 64514  # Kind cluster ASN
  
  # Two physical interfaces for dual-homing
  nics:
  - "toswitch"   # Connects to leafkind
  - "toswitch2"  # Connects to leafkind2
  
  # Four BGP neighbors (2 per ToR)
  neighbors:
  - asn: 64512
    address: "192.168.11.2"  # leafkind neighbor 1
  - asn: 64512
    address: "192.168.11.3"  # leafkind neighbor 2
  - asn: 64513
    address: "192.168.12.2"  # leafkind2 neighbor 1
  - asn: 64513
    address: "192.168.12.3"  # leafkind2 neighbor 2
  
  evpn:
    vtepcidr: "100.65.0.0/24"
```

This configuration validates:
- Multiple NICs (2 interfaces)
- Multiple neighbors (4 BGP sessions)
- Different ASNs (64512 and 64513)
- EVPN/VXLAN data plane

### Manual E2E Verification

Once the containerlab topology is deployed, you can manually verify multi-interface/neighbor functionality:

#### Step 1: Verify BGP Sessions

```bash
# Get router pod name
ROUTER_POD=$(kubectl get pod -n openperouter-system -l app=router -o jsonpath='{.items[0].metadata.name}')

# Check BGP summary - should show 4 neighbors
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp summary"

# Expected output shows:
# Neighbor        V AS    MsgRcvd MsgSent State
# 192.168.11.2    4 64512 ...     ...     Established
# 192.168.11.3    4 64512 ...     ...     Established
# 192.168.12.2    4 64513 ...     ...     Established
# 192.168.12.3    4 64513 ...     ...     Established

# Check detailed neighbor information
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp neighbors" | grep "BGP state"
# All should show "BGP state = Established"
```

#### Step 2: Verify Interfaces Moved to Router Namespace

```bash
# Check interfaces in router namespace
kubectl exec -n openperouter-system $ROUTER_POD -- ip link show

# Should see both toswitch and toswitch2 interfaces
# Example output:
# 5: toswitch@if6: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 ...
# 7: toswitch2@if8: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 ...

# Verify IP addresses on underlay interfaces
kubectl exec -n openperouter-system $ROUTER_POD -- ip addr show toswitch
kubectl exec -n openperouter-system $ROUTER_POD -- ip addr show toswitch2
```

#### Step 3: Verify EVPN/VXLAN Setup

```bash
# Check VTEP interface (loopback with allocated IP)
kubectl exec -n openperouter-system $ROUTER_POD -- ip addr show lo

# Should show IP from vtepcidr range (e.g., 100.65.0.1/24)

# Check EVPN routes
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp l2vpn evpn"

# Check VXLAN interfaces
kubectl exec -n openperouter-system $ROUTER_POD -- ip link show type vxlan
```

#### Step 4: Test Underlay Connectivity

```bash
# Ping BGP neighbors from router namespace
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.11.2
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.11.3
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.12.2
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.12.3

# All should succeed
```

#### Step 5: Test L3 Data Plane (End-to-End Connectivity)

```bash
# Create a test pod to validate routing
kubectl run test-pod --image=nicolaka/netshoot --command -- sleep 3600

# Get pod IP
POD_IP=$(kubectl get pod test-pod -o jsonpath='{.status.podIP}')
echo "Test pod IP: $POD_IP"

# Ping pod from external host on leafA (via EVPN overlay)
sudo containerlab exec -t kind clab-kind-hostA_default ping -c 3 $POD_IP

# Expected: Success (validates full path: hostA -> leafA -> spine -> leafkind -> OpenPERouter -> pod)

# Ping pod from external host on leafB (different leaf)
sudo containerlab exec -t kind clab-kind-hostB_red ping -c 3 $POD_IP

# Expected: Success (validates redundancy via leafB -> spine -> leafkind2 -> OpenPERouter -> pod)

# Trace route to see path
sudo containerlab exec -t kind clab-kind-hostA_default traceroute $POD_IP
```

#### Step 6: Verify Multi-Path Routing

```bash
# Check FRR routing table - should show multiple paths
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show ip route"

# Check EVPN routes learned from both ToRs
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp l2vpn evpn route"

# Verify both ToRs have learned routes
sudo containerlab exec -t kind clab-kind-leafkind vtysh -c "show bgp l2vpn evpn"
sudo containerlab exec -t kind clab-kind-leafkind2 vtysh -c "show bgp l2vpn evpn"
```

#### Step 7: Test Configuration Updates (Hot-Apply)

```bash
# Check router pod uptime before update
kubectl exec -n openperouter-system $ROUTER_POD -- uptime

# Add a fifth neighbor (should be hot-applied without restart)
kubectl patch underlay underlay -n openperouter-system --type merge -p '
spec:
  neighbors:
  - asn: 64512
    address: "192.168.11.2"
  - asn: 64512
    address: "192.168.11.3"
  - asn: 64513
    address: "192.168.12.2"
  - asn: 64513
    address: "192.168.12.3"
  - asn: 64514
    address: "192.168.13.2"
'

# Wait a few seconds for reconciliation
sleep 10

# Check uptime again - should NOT have reset (hot-apply worked)
kubectl exec -n openperouter-system $ROUTER_POD -- uptime

# Verify new neighbor appears in BGP config
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show running-config" | grep neighbor
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

## Troubleshooting Guide

### Common Issues and Solutions

#### Issue 1: BGP Sessions Stuck in "Idle" or "Connect" State

**Symptom**: `show bgp summary` shows neighbors in Idle/Connect state instead of Established

**Debug Steps**:
```bash
# Check BGP daemon logs
kubectl exec -n openperouter-system $ROUTER_POD -- cat /var/log/frr/bgpd.log | tail -50

# Look for connection errors like:
# - "Connection refused"
# - "Network unreachable"
# - "No route to host"

# Test basic IP connectivity to neighbor
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 3 192.168.11.2

# Check if interface is UP
kubectl exec -n openperouter-system $ROUTER_POD -- ip link show toswitch

# Verify neighbor is actually listening on BGP port
kubectl exec -n openperouter-system $ROUTER_POD -- nc -zv 192.168.11.2 179
```

**Common Causes & Solutions**:

1. **Interface not UP**: Ensure NIC names match actual interfaces on the node
   ```bash
   # Check what interfaces exist on worker node
   kubectl debug node/pe-kind-worker -it --image=nicolaka/netshoot -- ip link
   ```

2. **IP addressing issue**: Verify ToR and router have IPs in same subnet
   ```bash
   # Check IP on ToR side
   sudo containerlab exec -t kind clab-kind-leafkind ip addr show toswitch
   
   # Check IP on router side
   kubectl exec -n openperouter-system $ROUTER_POD -- ip addr show toswitch
   ```

3. **Firewall blocking BGP**: Check for iptables rules blocking TCP 179
   ```bash
   sudo iptables -L -n | grep 179
   ```

4. **Neighbor not configured**: Verify ToR switch has matching BGP config
   ```bash
   sudo containerlab exec -t kind clab-kind-leafkind vtysh -c "show running-config"
   ```

#### Issue 2: Webhook Validation Rejecting Valid Configuration

**Symptom**: `kubectl apply` fails with validation error

**Debug Steps**:
```bash
# View webhook logs
kubectl logs -n openperouter-system deployment/openperouter-webhook --tail=100

# Check for specific validation errors:
# - "duplicate neighbor address"
# - "duplicate nic name"
# - "local ASN must be different from remote ASN"
# - "at least one neighbor required"
# - "at least one nic required"

# Verify your YAML has unique values
kubectl apply -f underlay.yaml --dry-run=server -v=8
```

**Common Causes & Solutions**:

1. **Duplicate neighbor addresses**: Each neighbor must have unique IP
   ```yaml
   # WRONG
   neighbors:
   - asn: 64512
     address: "192.168.11.2"
   - asn: 64513
     address: "192.168.11.2"  # Duplicate!
   
   # CORRECT
   neighbors:
   - asn: 64512
     address: "192.168.11.2"
   - asn: 64513
     address: "192.168.12.2"  # Different IP
   ```

2. **ASN conflict**: Local ASN must differ from all neighbor ASNs
   ```yaml
   # WRONG
   spec:
     asn: 64514
     neighbors:
     - asn: 64514  # Same as local!
   
   # CORRECT
   spec:
     asn: 64514
     neighbors:
     - asn: 64512  # Different ASN
   ```

3. **Empty neighbors/nics**: At least one of each required
   ```yaml
   # WRONG
   neighbors: []
   
   # CORRECT
   neighbors:
   - asn: 64512
     address: "192.168.11.2"
   ```

#### Issue 3: Interfaces Not Appearing in Router Namespace

**Symptom**: `ip link show` in router pod doesn't show expected NICs

**Debug Steps**:
```bash
# Check controller logs for interface movement errors
kubectl logs -n openperouter-system deployment/openperouter-controller-manager | grep -i "interface\|nic"

# Look for errors like:
# - "failed to move interface"
# - "interface not found"
# - "interface already in namespace"

# Check if interface exists on host
kubectl debug node/pe-kind-worker -it --image=nicolaka/netshoot -- ip link show

# Check if interface is already in another namespace
kubectl debug node/pe-kind-worker -it --image=nicolaka/netshoot -- ip netns list
```

**Common Causes & Solutions**:

1. **Typo in NIC name**: Verify exact interface name (case-sensitive)
   ```bash
   # List actual interfaces on node
   kubectl debug node/pe-kind-worker -it --image=nicolaka/netshoot -- ip link
   ```

2. **Interface doesn't exist**: Node may not have that interface
   - Solution: Update Underlay spec with correct interface names

3. **Interface name validation failed**: Must match pattern `^[a-zA-Z][a-zA-Z0-9._-]*$`
   ```yaml
   # WRONG
   nics:
   - "1eth"  # Starts with number
   
   # CORRECT
   nics:
   - "eth1"
   ```

#### Issue 4: L3 Connectivity Fails (External Host Can't Reach Pod)

**Symptom**: Ping from external host to pod fails

**Debug Steps**:
```bash
# Verify BGP sessions are established (prerequisite)
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp summary"

# Check if EVPN routes are being advertised
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show bgp l2vpn evpn"

# Verify VTEP interface has IP
kubectl exec -n openperouter-system $ROUTER_POD -- ip addr show lo | grep inet

# Check if external host has route to pod network
sudo containerlab exec -t kind clab-kind-hostA_default ip route

# Test connectivity at each hop:
# 1. External host -> leaf
sudo containerlab exec -t kind clab-kind-hostA_default ping -c 1 <leaf_IP>

# 2. Leaf -> ToR
sudo containerlab exec -t kind clab-kind-leafA ping -c 1 <tor_IP>

# 3. ToR -> OpenPERouter
sudo containerlab exec -t kind clab-kind-leafkind ping -c 1 <vtep_IP>

# 4. OpenPERouter -> Pod
kubectl exec -n openperouter-system $ROUTER_POD -- ping -c 1 <pod_IP>
```

**Common Causes & Solutions**:

1. **EVPN not configured**: Missing L3VNI or L2VNI configuration
   ```bash
   kubectl get l3vni -n openperouter-system
   # Should show at least one VNI
   ```

2. **Routes not propagated**: Check spine switch is propagating routes
   ```bash
   sudo containerlab exec -t kind clab-kind-spine vtysh -c "show ip route"
   ```

3. **VRF misconfiguration**: Verify VRF and VNI settings match across config
   ```bash
   kubectl exec -n openperouter-system $ROUTER_POD -- ip vrf list
   ```

#### Issue 5: Containerlab Topology Fails to Deploy

**Symptom**: `containerlab deploy` exits with error

**Debug Steps**:
```bash
# Check containerlab version
containerlab version
# Must be >= 0.74.1 for group support

# Validate topology file syntax
sudo containerlab inspect -t kind.clab.yml

# Check Docker is running
docker ps

# View detailed error messages
sudo containerlab deploy -t kind.clab.yml --debug
```

**Common Causes & Solutions**:

1. **Old containerlab version**: Upgrade to 0.74.1+
   ```bash
   bash -c "$(curl -sL https://get.containerlab.dev)"
   ```

2. **Port conflicts**: Another service using required ports
   ```bash
   # Check for port conflicts
   sudo ss -tlnp | grep -E ':(179|6443|2379)'
   ```

3. **Insufficient permissions**: Must run with sudo
   ```bash
   sudo containerlab deploy -t kind.clab.yml
   ```

#### Issue 6: E2E Tests Fail with Timeout

**Symptom**: `go test ./e2etests/tests` fails with timeout errors

**Debug Steps**:
```bash
# Increase test timeout
go test ./e2etests/tests -v -timeout 30m

# Check if topology is fully deployed
sudo containerlab inspect -t kind.clab.yml

# Verify all containers are running
docker ps -a | grep clab-kind

# Check Kind cluster is accessible
kubectl get nodes

# View test output for specific failure point
go test ./e2etests/tests -v -run TestSessions 2>&1 | tee test.log
```

**Common Causes & Solutions**:

1. **Slow system**: Increase timeout in test flags
   ```bash
   go test ./e2etests/tests -timeout 20m
   ```

2. **BGP convergence delay**: Tests may need to wait longer for BGP
   - Check test code for hardcoded sleep values

3. **Resource constraints**: Ensure sufficient CPU/memory
   ```bash
   # Check system resources
   free -h
   top
   ```

### General Debugging Tips

#### Enable Debug Logging

```bash
# Controller debug logs
kubectl set env -n openperouter-system deployment/openperouter-controller-manager LOG_LEVEL=debug

# Watch logs in real-time
kubectl logs -n openperouter-system deployment/openperouter-controller-manager -f
```

#### Inspect FRR Configuration

```bash
# View generated FRR config
kubectl exec -n openperouter-system $ROUTER_POD -- cat /etc/frr/frr.conf

# Expected multi-neighbor config:
# router bgp 64514
#   neighbor 192.168.11.2 remote-as 64512
#   neighbor 192.168.11.3 remote-as 64512
#   neighbor 192.168.12.2 remote-as 64513
#   neighbor 192.168.12.3 remote-as 64513

# View running config (may differ from file if hot-applied)
kubectl exec -n openperouter-system $ROUTER_POD -- vtysh -c "show running-config"
```

#### Clean Up and Restart

```bash
# Destroy containerlab topology
cd clab/singlecluster
sudo containerlab destroy -t kind.clab.yml --cleanup

# Redeploy from scratch
sudo containerlab deploy -t kind.clab.yml

# Delete and recreate Underlay resource
kubectl delete underlay underlay -n openperouter-system
kubectl apply -f underlay.yaml
```

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
