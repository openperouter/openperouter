#!/usr/bin/env bash
# Wrapper entrypoint that initializes OVS before starting kind node
# This ensures OVS is available when Kubernetes components start

set -euo pipefail

# Initialize OVS
/usr/local/bin/init-ovs.sh

# Execute the original kind entrypoint with all arguments
exec /usr/local/bin/entrypoint "${@}"
