#!/usr/bin/env bash
set -euo pipefail

sudo apt-get update

kernel_version=$(uname -r)
package="linux-modules-extra-${kernel_version}"
# Newer Azure kernels ship all modules in the base package.
if ! apt-cache show "$package" > /dev/null 2>&1; then
    package="linux-modules-${kernel_version}"
fi

sudo apt-get install -y "$package"

# Kind nodes share the host kernel and use a no-op modprobe wrapper.
for module in dummy vrf bridge veth vxlan 8021q macvlan openvswitch sch_fq_codel; do
    sudo modprobe "$module"
done
