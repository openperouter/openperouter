# Custom Kind Node Image with OpenVSwitch

This directory contains the build infrastructure for a custom kind node image with OpenVSwitch (OVS) pre-installed. This eliminates the need to install OVS at runtime and improves reliability in development and CI/CD environments.

## Quick Start

### Using the Pre-built Image

The easiest way to use the custom kind image is to pull it from the registry:

```bash
export NODE_IMAGE=quay.io/openperouter/kind-node-ovs:v1.31.4
make deploy
```

The image is automatically used by default when you run `make deploy`.

### Building Locally

To build the image locally for testing or development:

```bash
cd hack/kind-node-image
./build.sh
```

### Building with Custom Kind Version

```bash
KIND_NODE_VERSION=v1.32.0 ./build.sh
```

## Image Details

- **Base Image:** kindest/node (official kind node image)
- **Current Version:** v1.31.4
- **OVS Version:** Ubuntu default from apt-get
- **OVS Socket:** `/var/run/openvswitch/db.sock`
- **Registry:** quay.io/openperouter/kind-node-ovs
- **Supported Architectures:** linux/amd64, linux/arm64

## What's Included

This custom image extends the official kind node image with:

- **openvswitch-switch** package
- **openvswitch-common** package
- Pre-initialized OVS database
- Systemd service configuration for automatic OVS startup
- Kubelet dependency configuration to ensure OVS starts first

## Architecture

The custom image works by:

1. **Build-time installation:** OVS packages are installed during image build
2. **Database initialization:** OVS database is created at build time
3. **Systemd integration:** OVS services are configured to start automatically via systemd
4. **Service dependencies:** Kubelet is configured to start only after OVS services are running

This approach provides several benefits over runtime installation:
- Faster cluster startup (no apt-get install delay)
- No network dependency during cluster creation
- Consistent OVS version across environments
- Eliminates apt-get failures in CI/CD

## Building and Pushing

### Local Build (Single Architecture)

Build for your current platform:

```bash
make kind-node-image-build
```

Or directly:

```bash
cd hack/kind-node-image
./build.sh
```

### Push to Registry (Multi-Architecture)

Build and push multi-arch images (requires quay.io credentials):

```bash
make kind-node-image-push
```

Or directly:

```bash
cd hack/kind-node-image
./push.sh
```

### Custom Configuration

Both scripts support environment variables for customization:

```bash
# Custom kind version
KIND_NODE_VERSION=v1.32.0 ./build.sh

# Custom repository
IMG_REPO=docker.io/myorg ./build.sh

# Custom platforms for multi-arch build
PLATFORMS=linux/amd64,linux/arm64,linux/arm/v7 ./push.sh

# Use podman instead of docker
CONTAINER_ENGINE=podman ./build.sh
```

## CI/CD

Images are automatically built and pushed via GitHub Actions on:

- **Pushes to main branch** (when files in this directory change)
- **Weekly schedule** (Monday 2 AM UTC - to pick up security updates)
- **Manual workflow dispatch**

The CI workflow:
1. Builds images for linux/amd64 and linux/arm64
2. Tests OVS functionality on amd64
3. Pushes to quay.io/openperouter/kind-node-ovs
4. Tags with both version tag (e.g., v1.31.4) and `latest`

## Usage Examples

### Create a Kind Cluster

```bash
# Uses custom image automatically
make deploy

# Or specify explicitly
export NODE_IMAGE=quay.io/openperouter/kind-node-ovs:v1.31.4
kind create cluster --name test
```

### Verify OVS is Running

```bash
# Check OVS status in running cluster
docker exec pe-kind-control-plane ovs-vsctl show

# Check OVS processes
docker exec pe-kind-control-plane pgrep ovsdb-server
docker exec pe-kind-control-plane pgrep ovs-vswitchd
```

### Create an OVS Bridge

```bash
# The ensure-ovs-bridge-kind.sh script works with the pre-installed OVS
./hack/ensure-ovs-bridge-kind.sh
```

## Troubleshooting

### Verify OVS is Running

If you're having issues with OVS, check that the services are running:

```bash
# Check OVS status
docker exec <node-name> ovs-vsctl show

# Check service status (if systemd is available)
docker exec <node-name> systemctl status ovsdb-server
docker exec <node-name> systemctl status ovs-vswitchd

# Check OVS logs
docker exec <node-name> journalctl -u ovsdb-server -n 50
docker exec <node-name> journalctl -u ovs-vswitchd -n 50
```

### Test OVS Functionality

```bash
# Start a test container
docker run -d --privileged --name test-ovs quay.io/openperouter/kind-node-ovs:v1.31.4
# Wait for systemd to start OVS
sleep 10
# Test OVS
docker exec test-ovs ovs-vsctl show
# Cleanup
docker rm -f test-ovs
```

### Build Fails

If the build fails:

1. Check that the base kind image exists: `docker pull kindest/node:v1.31.4`
2. Ensure you have sufficient disk space
3. Check Docker/Podman is running correctly
4. Review build logs for specific errors

### Push Fails

If pushing to quay.io fails:

1. Ensure you're logged in: `docker login quay.io`
2. Verify you have write access to quay.io/openperouter
3. Check that buildx is installed: `docker buildx version`

## Maintenance

### Updating to a New Kubernetes Version

When a new kind release is available:

1. Check for new kind releases: https://github.com/kubernetes-sigs/kind/releases
2. Update `KIND_NODE_VERSION` in:
   - `Makefile` (default value)
   - `.github/workflows/build-kind-node-image.yaml`
   - This README
3. Build and test the new image locally
4. Push to registry
5. Update references in `hack/kind.sh` and CI workflows

### Security Updates

The image is automatically rebuilt weekly to pick up security patches from Ubuntu repositories. This ensures the OVS packages and base image stay up-to-date without manual intervention.

To manually trigger a rebuild:
1. Go to GitHub Actions
2. Select "Build Kind Node Image" workflow
3. Click "Run workflow"

### Monitoring Image Builds

Check the build status in GitHub Actions:
- https://github.com/openperouter/openperouter/actions/workflows/build-kind-node-image.yaml

## Related Files

- `/hack/kind.sh` - Uses NODE_IMAGE variable (defaults to custom image)
- `/hack/ensure-ovs-bridge-kind.sh` - Creates OVS bridges (works with pre-installed OVS)
- `/hack/install-ovs-kind.sh` - DEPRECATED (kept for compatibility with standard kindest/node)
- `/.github/workflows/ci.yaml` - CI workflow that uses the custom image
- `/.github/workflows/build-kind-node-image.yaml` - Workflow that builds and publishes the image

## Frequently Asked Questions

### Why create a custom image?

The custom image eliminates runtime OVS installation, which:
- Reduces cluster startup time by 30-45 seconds
- Removes network dependency during cluster creation
- Prevents apt-get failures in CI/CD
- Ensures consistent OVS versions across environments

### Can I still use the standard kindest/node image?

Yes! You can override the image:

```bash
export NODE_IMAGE=kindest/node:v1.30.0
make deploy
```

### How do I update the OVS version?

The image uses Ubuntu's default OVS version from apt-get. To pin a specific version, modify the Dockerfile:

```dockerfile
apt-get install -y openvswitch-switch=2.17.x
```

### Does this work on M1/M2 Macs?

Yes! The multi-arch build includes linux/arm64 support for Apple Silicon.

### How often is the image rebuilt?

- Automatically every Monday at 2 AM UTC (weekly schedule)
- On every push to main that modifies files in this directory
- Manually via workflow dispatch when needed
