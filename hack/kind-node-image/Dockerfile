# Custom Kind Node Image with OpenVSwitch
# Based on official kindest/node image with OVS pre-installed for OpenPERouter

ARG KIND_NODE_VERSION=v1.32.2
FROM kindest/node:${KIND_NODE_VERSION}

# Re-declare ARG to make it available after FROM
ARG KIND_NODE_VERSION

# Install OpenVSwitch packages
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
        openvswitch-switch \
        openvswitch-common 

# Create necessary OVS directories
RUN mkdir -p /var/run/openvswitch /var/log/openvswitch /etc/openvswitch

# Create OVS configuration file expected by systemd services
RUN echo "OVS_USER_ID=root:root" > /etc/openvswitch/default.conf

# Initialize OVS database at build time
RUN ovsdb-tool create /etc/openvswitch/conf.db \
    /usr/share/openvswitch/vswitch.ovsschema

# Configure systemd to start OVS services automatically
RUN systemctl enable ovsdb-server.service && \
    systemctl enable ovs-vswitchd.service

# Create a dummy modprobe wrapper to prevent kernel module loading in containers
# ovs-kmod-ctl calls modprobe directly, so we intercept it by placing a wrapper
# in /usr/local/bin which comes before /usr/sbin in PATH
RUN printf '#!/bin/sh\n# Dummy modprobe for containerized OVS - always succeeds\nexit 0\n' \
    > /usr/local/bin/modprobe && \
    chmod +x /usr/local/bin/modprobe

# Create systemd service override to ensure OVS starts before kubelet
# This prevents race conditions where pods try to use OVS before it's ready
RUN mkdir -p /etc/systemd/system/kubelet.service.d && \
    printf "[Unit]\nAfter=ovsdb-server.service ovs-vswitchd.service\nRequires=ovsdb-server.service ovs-vswitchd.service\n" \
    > /etc/systemd/system/kubelet.service.d/10-ovs-dependency.conf

# Add Debian Testing repository for Podman 5.x
RUN printf 'Types: deb\nURIs: http://deb.debian.org/debian\nSuites: testing\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n' \
    > /etc/apt/sources.list.d/debian-testing.sources

# Pin testing to low priority so only explicitly requested packages are pulled from it
RUN printf "Package: *\nPin: release a=testing\nPin-Priority: 100\n" \
    > /etc/apt/preferences.d/testing.pref

RUN apt-get update -y && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y -t testing \
    -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" \
    podman && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*


# Add labels for image identification and metadata
LABEL org.opencontainers.image.source="https://github.com/openperouter/openperouter" \
      org.opencontainers.image.description="Kind node image with OpenVSwitch and Podman 5.x pre-installed for OpenPERouter quadlet support" \
      org.opencontainers.image.vendor="OpenPERouter" \
      ovs.version="ubuntu-default" \
      podman.version="5.x-debian-testing" \
      podman.quadlet.support="true" \
      kind.base.version="${KIND_NODE_VERSION}"

# Note: We rely on systemd to start OVS services automatically (enabled above)
# DO NOT override ENTRYPOINT - kind needs its default entrypoint for proper init
