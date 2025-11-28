# Use the latest kind node image as base
FROM kindest/node:v1.34.0

# Install Podman 4.4+ from the official Kubic unstable repository (for quadlet support)
RUN apt-get update && \
    apt-get install -y curl gnupg2 software-properties-common && \
    mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://download.opensuse.org/repositories/devel:kubic:libcontainers:unstable/xUbuntu_22.04/Release.key | gpg --dearmor -o /etc/apt/keyrings/devel_kubic_libcontainers_unstable.gpg && \
    echo "deb [signed-by=/etc/apt/keyrings/devel_kubic_libcontainers_unstable.gpg] https://download.opensuse.org/repositories/devel:kubic:libcontainers:unstable/xUbuntu_22.04/ /" > /etc/apt/sources.list.d/devel:kubic:libcontainers:unstable.list && \
    apt-get update && \
    apt-get install -y podman && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Configure podman for rootless operation
#RUN echo 'unqualified-search-registries = ["docker.io"]' > /etc/containers/registries.conf && \
#    mkdir -p /etc/containers && \
#    echo -e '[storage]\ndriver = "overlay"\n[storage.options]\nmount_program = "/usr/bin/fuse-overlayfs"' > /etc/containers/storage.conf

# Ensure podman works in the container environment
RUN mkdir -p /var/lib/containers
