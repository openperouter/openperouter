# Custom Kind Node Image with OpenVSwitch
# Based on official kindest/node image with OVS pre-installed for OpenPERouter

ARG KIND_NODE_VERSION=v1.31.4
FROM kindest/node:${KIND_NODE_VERSION}

# Install OpenVSwitch packages
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
        openvswitch-switch \
        openvswitch-common && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create necessary OVS directories
RUN mkdir -p /var/run/openvswitch /var/log/openvswitch /etc/openvswitch

# Initialize OVS database at build time
RUN ovsdb-tool create /etc/openvswitch/conf.db \
    /usr/share/openvswitch/vswitch.ovsschema

# Configure systemd to start OVS services automatically
RUN systemctl enable ovsdb-server.service && \
    systemctl enable ovs-vswitchd.service

# Create systemd service override to ensure OVS starts before kubelet
# This prevents race conditions where pods try to use OVS before it's ready
RUN mkdir -p /etc/systemd/system/kubelet.service.d && \
    printf "[Unit]\nAfter=ovsdb-server.service ovs-vswitchd.service\nRequires=ovsdb-server.service ovs-vswitchd.service\n" \
    > /etc/systemd/system/kubelet.service.d/10-ovs-dependency.conf

# Add labels for image identification and metadata
LABEL org.opencontainers.image.source="https://github.com/openperouter/openperouter" \
      org.opencontainers.image.description="Kind node image with OpenVSwitch pre-installed for OpenPERouter" \
      org.opencontainers.image.vendor="OpenPERouter" \
      ovs.version="ubuntu-default" \
      kind.base.version="${KIND_NODE_VERSION}"

# Note: We rely on systemd to start OVS services automatically (enabled above)
# DO NOT override ENTRYPOINT - kind needs its default entrypoint for proper init
