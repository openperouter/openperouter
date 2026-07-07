---
weight: 56
title: "Troubleshooting Resiliency"
description: "Recovery procedures for the named network namespace and router resiliency"
icon: "article"
date: "2026-05-04T00:00:00+02:00"
lastmod: "2026-05-04T00:00:00+02:00"
toc: true
---

Most FRR failures are self-healing: the container restarts, re-enters the persistent named namespace, and BGP Graceful Restart bridges the control-plane gap. This page covers scenarios that require operator intervention.

## Checking the Named Namespace

Verify that the named namespace exists and is healthy:

```bash
# Check if the namespace exists
ip netns list | grep perouter

# List interfaces inside the namespace
ip netns exec perouter ip link show

# Check routes inside the namespace
ip netns exec perouter ip route show

# Check FRR BGP session status
kubectl exec -n openperouter-system <router-pod> -c frr -- \
  vtysh -c "show bgp summary"

# Check EVPN route exchange
kubectl exec -n openperouter-system <router-pod> -c frr -- \
  vtysh -c "show bgp l2vpn evpn summary"
```

## Recovery: Full Namespace Rebuild

**When to use**: The node's networking state is out of sync — stale interfaces, wrong IPs, partial failure, or manual interference.

**Impact**: Data-plane disruption (~10-25 seconds). All traffic through VNIs on that node is interrupted during the rebuild.

```bash
# 1. Delete the named namespace (destroys all kernel objects inside it).
#    The underlay NIC is returned to the host namespace.
ip netns delete perouter

# 2. Delete the router pod on the affected node to trigger a DaemonSet replacement.
kubectl delete pod -n openperouter-system <router-pod-name>

# 3. The controller will automatically:
#    - Detect the missing namespace on the next reconciliation
#    - Recreate it via EnsureNamespace()
#    - Re-provision all interfaces from CRD state
#    - The new router pod enters the fresh namespace
```

## Recovery: FRR Container Not Starting

**Symptoms**: Router pod stuck in `CrashLoopBackOff`. FRR container logs show `Waiting for named netns /var/run/netns/perouter...`.

**Cause**: The controller has not yet created the named namespace. The controller pod may not be ready, or it may be failing during reconciliation.

**Diagnosis**:

```bash
# Check the controller pod status
kubectl get pods -n openperouter-system -l app=openperouter-controller

# Check controller logs for errors
kubectl logs -n openperouter-system <controller-pod> --tail=50
```

**Resolution**: Fix the controller issue. Once the controller is running and reconciling, it creates the namespace and FRR will start.

## Recovery: Underlay NIC Missing

**Symptoms**: Controller logs show a non-recoverable error. BGP sessions are not establishing.

**Cause**: The physical NIC configured in `spec.interfaces` is not available on the node — cable unplugged, driver issue, or NIC renamed.

**Diagnosis**:

```bash
# Check if the NIC exists on the host
ip link show <nic-name>

# Check controller logs for the specific error
kubectl logs -n openperouter-system <controller-pod> | grep -i "non-recoverable\|underlay"
```

**Resolution**: Fix the hardware or driver issue. The controller will detect the NIC on the next reconciliation and move it into the namespace.

## Verifying Recovery

After any recovery procedure, verify that all components are back to a healthy state:

```bash
# All pods should be Running and Ready
kubectl get pods -n openperouter-system

# The named namespace should exist and have interfaces
ip netns exec perouter ip link show

# BGP sessions should be Established
kubectl exec -n openperouter-system <router-pod> -c frr -- \
  vtysh -c "show bgp summary"

# EVPN routes should be exchanged
kubectl exec -n openperouter-system <router-pod> -c frr -- \
  vtysh -c "show bgp l2vpn evpn summary"
```
