#!/bin/env bash
#
# Generate systemd unit files for OpenPerouter pods
#
# Environment variables:
#   IMAGE            - Router and FRR image (default: quay.io/openperouter/router:main)
#   SKIP_OVS_MOUNT   - Set to "true" to skip OVS mount when Open vSwitch is not installed
#
# Example usage:
#   ./generate_systemd.sh                          # Generate with OVS mount, use hostname
#   SKIP_OVS_MOUNT=true ./generate_systemd.sh      # Generate without OVS mount

set -euo pipefail

ROUTER_IMAGE="${IMAGE:-quay.io/openperouter/router:main}"
FRR_IMAGE="${IMAGE:-quay.io/frrouting/frr:10.2.1}"

# Set to "true" to skip OVS mount (useful when Open vSwitch is not installed)
SKIP_OVS_MOUNT="${SKIP_OVS_MOUNT:-false}"

if [ "$SKIP_OVS_MOUNT" = "true" ]; then
    echo "Skipping OVS mount (Open vSwitch not required)"
fi


# Create temporary directories for pod generation
# These will be used instead of real host paths, then replaced in the generated systemd files
TEMP_BASE=$(mktemp -d)
echo "Using temporary directory: ${TEMP_BASE}"

mkdir -p "${TEMP_BASE}/run/containerd"
mkdir -p "${TEMP_BASE}/run/netns"
mkdir -p "${TEMP_BASE}/etc/perouter/frr"
mkdir -p "${TEMP_BASE}/var/lib/hostbridge"
mkdir -p "${TEMP_BASE}/var/lib/openperouter"
mkdir -p "${TEMP_BASE}/proc"
mkdir -p "${TEMP_BASE}/run/dbus"

# Only create OVS directory if not skipping OVS mount
if [ "$SKIP_OVS_MOUNT" != "true" ]; then
    mkdir -p "${TEMP_BASE}/var/run/openvswitch"
fi

touch "${TEMP_BASE}/run/containerd/containerd.sock"
touch "${TEMP_BASE}/run/dbus/system_bus_socket"

cleanup() {
    echo "Cleaning up temporary directory: ${TEMP_BASE}"
    rm -rf "${TEMP_BASE}"
}
trap cleanup EXIT

# Clean up any existing pods/containers first
podman pod rm -f routerpod controllerpod 2>/dev/null || true

podman pod create --share=+pid --name=routerpod 
podman create --pod=routerpod --name=frr \
	--pidfile=/etc/perouter/frr/frr.pid \
	--cap-add=CAP_NET_BIND_SERVICE,CAP_NET_ADMIN,CAP_NET_RAW,CAP_SYS_ADMIN \
	-e TINI_SUBREAPER=true \
	-v=frr-sockets:/var/run/frr:Z \
	-v=frrconfig:/etc/frr:Z \
	--entrypoint=/bin/bash \
	"$FRR_IMAGE" \
	-c "for i in {1..10}; do test -f /etc/frr/daemons && break || sleep 5; done && chmod -R a+rw /var/run/frr && /sbin/tini -- /usr/lib/frr/docker-start & attempts=0; until [[ -f /etc/frr/frr.log || \$attempts -eq 60 ]]; do sleep 1; attempts=\$(( \$attempts + 1 )); done; tail -f /etc/frr/frr.log"

podman create --pod=routerpod --name=reloader \
	-v=frrconfig:/etc/frr:Z \
	-v=frr-sockets:/var/run/frr:Z \
	-v="${TEMP_BASE}/etc/perouter/frr":/etc/perouter:Z \
	-v=reloader:/etc/frr_reloader:Z \
	--entrypoint=/bin/bash \
	"$FRR_IMAGE" \
	-c "for i in {1..10}; do test -f /etc/frr_reloader/reloader && break || sleep 5; done && /etc/frr_reloader/reloader --frrconfig=/etc/perouter/frr.conf --loglevel=debug --unixsocket /etc/perouter/frr.socket"

podman create --pod=routerpod --name=copier \
	-v=frrconfig:/etc/frr:Z \
	-v=reloader:/etc/frr_reloader:Z \
	--entrypoint=/bin/sh \
	"$ROUTER_IMAGE" \
	-c "cp -rLf /usr/share/openperouter/frr/* /etc/frr && chmod -R a+rw /etc/frr && \
	 cp /reloader /etc/frr_reloader/reloader && chmod -R a+rw /etc/frr_reloader && sleep infinity"


podman pod create --name=controllerpod

# Build optional mount arguments
OPTIONAL_MOUNTS=()
if [ "$SKIP_OVS_MOUNT" != "true" ]; then
	OPTIONAL_MOUNTS+=("-v" "${TEMP_BASE}/var/run/openvswitch:/var/run/openvswitch:rshared")
fi

podman create --pod=controllerpod --name=controller \
	-v="${TEMP_BASE}/run/containerd/containerd.sock":/run/containerd/containerd.sock:rshared \
	-v="${TEMP_BASE}/run/netns":/run/netns:rshared \
	-v="${TEMP_BASE}/etc/perouter/frr":/etc/perouter/frr:rshared \
	-v="${TEMP_BASE}/var/lib/hostbridge":/shared:rshared \
	-v="${TEMP_BASE}/var/lib/openperouter":/etc/openperouter:ro \
	-v="${TEMP_BASE}/proc":/hostproc:ro \
	-v="${TEMP_BASE}/run/dbus/system_bus_socket":/host/dbus/system_bus_socket:rw \
	"${OPTIONAL_MOUNTS[@]}" \
	-e KUBECONFIG=/shared/kubeconfig \
	--privileged \
	--network=host \
	--cap-add=CAP_NET_BIND_SERVICE,CAP_NET_ADMIN,CAP_NET_RAW,CAP_SYS_ADMIN \
	--pid=host \
	-t "$ROUTER_IMAGE" \
	--loglevel debug --frrconfig /etc/perouter/frr/frr.conf --pid-path /etc/perouter/frr/frr.pid --reloader-socket /etc/perouter/frr/frr.socket \
	--mode host --nodename "NODENAME"

# Generate systemd unit files for both pods
podman generate systemd --new --files --name routerpod
podman generate systemd --new --files --name controllerpod

# Replace temporary base path with root in generated systemd files
# This converts paths like /tmp/tmp.XXXXX/run/netns to /run/netns
# and /tmp/tmp.XXXXX/var/lib/openperouter to /var/lib/openperouter
echo "Replacing temporary paths with actual host paths..."
sed -i "s|${TEMP_BASE}||g" container-controller.service container-reloader.service

# Use the hostname systemd expansion %H inside the unit to configure the 
# nodename, we may need to improve this mechanism for scenarios where ndename 
# is not the hostname
sed -i 's|NODENAME|%H|g' container-controller.service container-reloader.service

# Clean up the temporary pods and containers
# The --new flag ensures systemd units will create/remove them on start/stop
podman pod rm -f routerpod controllerpod

