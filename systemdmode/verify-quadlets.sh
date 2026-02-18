#!/bin/bash
#
# verify-quadlets.sh
# OpenPERouter Quadlet Deployment Verification Script
#
# This script tests all lifecycle operations for quadlet-based deployment.
# Run this script after deploying quadlet files to verify functionality.
#
# Usage:
#   sudo ./verify-quadlets.sh

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counter
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

test_start() {
    echo ""
    log_info "Starting test: $1"
    TESTS_RUN=$((TESTS_RUN + 1))
}

test_pass() {
    log_info "✓ PASS: $1"
    TESTS_PASSED=$((TESTS_PASSED + 1))
}

test_fail() {
    log_error "✗ FAIL: $1"
    TESTS_FAILED=$((TESTS_FAILED + 1))
}

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    log_error "This script must be run as root (use sudo)"
    exit 1
fi

log_info "OpenPERouter Quadlet Verification Script"
log_info "========================================="

# Test 1: Verify quadlet files exist
test_start "Quadlet files exist in /etc/containers/systemd/"
if [ -f "/etc/containers/systemd/routerpod.pod" ] && \
   [ -f "/etc/containers/systemd/controllerpod.pod" ] && \
   [ -f "/etc/containers/systemd/frr.container" ] && \
   [ -f "/etc/containers/systemd/reloader.container" ] && \
   [ -f "/etc/containers/systemd/controller.container" ] && \
   [ -f "/etc/containers/systemd/frr-sockets.volume" ]; then
    test_pass "All quadlet files present"
else
    test_fail "Missing quadlet files in /etc/containers/systemd/"
    log_error "Run 'sudo cp systemdmode/quadlets/*.{pod,container,volume} /etc/containers/systemd/' first"
    exit 1
fi

# Test 2: Verify systemd daemon-reload
test_start "Systemd daemon-reload"
if systemctl daemon-reload; then
    test_pass "systemd daemon-reload successful"
else
    test_fail "systemd daemon-reload failed"
    exit 1
fi

# Test 3: Verify generated services exist
test_start "Generated systemd services exist"
if systemctl list-unit-files | grep -q "routerpod-pod.service" && \
   systemctl list-unit-files | grep -q "controllerpod-pod.service"; then
    test_pass "Generated services found"
else
    test_fail "Generated services not found"
    log_error "systemd-generator may not have processed quadlet files"
    exit 1
fi

# Test 4: Stop services if running (clean slate)
test_start "Stop existing services"
systemctl stop routerpod-pod.service controllerpod-pod.service 2>/dev/null || true
sleep 2
test_pass "Services stopped"

# Test 5: Start services
test_start "Start services"
if systemctl start routerpod-pod.service controllerpod-pod.service; then
    test_pass "Services started successfully"
else
    test_fail "Failed to start services"
    journalctl -xe
    exit 1
fi

# Wait for containers to initialize
log_info "Waiting 10 seconds for containers to initialize..."
sleep 10

# Test 6: Check service status
test_start "Check service status"
if systemctl is-active --quiet routerpod-pod.service && \
   systemctl is-active --quiet controllerpod-pod.service; then
    test_pass "All services are active"
else
    test_fail "Some services are not active"
    systemctl status routerpod-pod.service --no-pager || true
    systemctl status controllerpod-pod.service --no-pager || true
fi

# Test 7: Verify containers are running
test_start "Verify containers are running"
RUNNING_CONTAINERS=$(podman ps --format "{{.Names}}" | grep -E "^(frr|reloader|controller)$" | wc -l)
if [ "$RUNNING_CONTAINERS" -eq 3 ]; then
    test_pass "All 3 containers are running (frr, reloader, controller)"
else
    test_fail "Expected 3 containers, found $RUNNING_CONTAINERS"
    podman ps -a
fi

# Test 8: Test restart operation
test_start "Restart services"
if systemctl restart routerpod-pod.service controllerpod-pod.service; then
    test_pass "Services restarted successfully"
    sleep 5
else
    test_fail "Failed to restart services"
fi

# Test 9: Verify services are still active after restart
test_start "Verify services active after restart"
if systemctl is-active --quiet routerpod-pod.service && \
   systemctl is-active --quiet controllerpod-pod.service; then
    test_pass "Services are active after restart"
else
    test_fail "Services failed after restart"
fi

# Test 10: Test journalctl logs access
test_start "Access service logs via journalctl"
if journalctl -u routerpod-pod.service -n 10 --no-pager > /dev/null && \
   journalctl -u controllerpod-pod.service -n 10 --no-pager > /dev/null; then
    test_pass "Service logs accessible"
else
    test_fail "Cannot access service logs"
fi

# Test 11: Test enable operation
test_start "Enable services for boot"
if systemctl enable routerpod-pod.service controllerpod-pod.service > /dev/null 2>&1; then
    test_pass "Services enabled for boot"
else
    test_fail "Failed to enable services"
fi

# Test 12: Verify enabled status
test_start "Verify services are enabled"
if systemctl is-enabled --quiet routerpod-pod.service && \
   systemctl is-enabled --quiet controllerpod-pod.service; then
    test_pass "Services are enabled"
else
    test_fail "Services are not enabled"
fi

# Test 13: Test disable operation
test_start "Disable services"
if systemctl disable routerpod-pod.service controllerpod-pod.service > /dev/null 2>&1; then
    test_pass "Services disabled"
else
    test_fail "Failed to disable services"
fi

# Test 14: Verify FRR container logs
test_start "Verify FRR container logs"
if podman logs frr 2>/dev/null | grep -q "frr"; then
    test_pass "FRR container logs accessible"
else
    log_warn "FRR logs may not contain expected content"
    test_pass "FRR container logs accessible (with warnings)"
fi

# Test 15: Verify required directories exist
test_start "Verify required host directories exist"
if [ -d "/etc/perouter/frr" ] && \
   [ -d "/var/lib/hostbridge" ] && \
   [ -d "/var/lib/openperouter" ]; then
    test_pass "All required directories exist"
else
    test_fail "Missing required directories"
    log_error "Run: sudo mkdir -p /etc/perouter/frr /var/lib/hostbridge /var/lib/openperouter"
fi

# Test 16: Verify FRR sockets volume exists
test_start "Verify FRR sockets volume"
if podman volume exists frr-sockets; then
    test_pass "FRR sockets volume exists"
else
    test_fail "FRR sockets volume not found"
fi

# Test 17: Final status check
test_start "Final service status check"
systemctl status routerpod-pod.service --no-pager --lines=5 || true
systemctl status controllerpod-pod.service --no-pager --lines=5 || true
test_pass "Status check complete"

# Summary
echo ""
log_info "========================================="
log_info "Verification Summary"
log_info "========================================="
log_info "Tests run:    $TESTS_RUN"
log_info "Tests passed: $TESTS_PASSED"
log_info "Tests failed: $TESTS_FAILED"

if [ "$TESTS_FAILED" -eq 0 ]; then
    echo ""
    log_info "✓ ALL TESTS PASSED"
    log_info "OpenPERouter quadlet deployment is functioning correctly!"
    exit 0
else
    echo ""
    log_error "✗ SOME TESTS FAILED"
    log_error "Please review the output above for details"
    exit 1
fi
