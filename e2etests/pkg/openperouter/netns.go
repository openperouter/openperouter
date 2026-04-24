// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

const namedNetns = "perouter"

// NamedNetnsExists checks whether /var/run/netns/perouter is present on nodeName.
func NamedNetnsExists(nodeName string) (bool, error) {
	exec := executor.ForContainer(nodeName)
	out, err := exec.Exec("ip", "netns", "list")
	if err != nil {
		return false, err
	}
	// Each line of "ip netns list" is "<name>" or "<name> (id: N)".
	// Use exact name comparison to avoid "perouter" matching inside "openperouter".
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == namedNetns {
			return true, nil
		}
	}
	return false, nil
}

// NamedNetnsHasInterfaceType checks whether the named netns contains at least one
// interface of the given link type (e.g. "vrf", "bridge", "vxlan").
func NamedNetnsHasInterfaceType(nodeName, linkType string) (bool, error) {
	exec := executor.ForContainer(nodeName)
	out, err := exec.Exec("ip", "netns", "exec", namedNetns, "ip", "link", "show", "type", linkType)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// DeleteNamedNetns runs "ip netns delete perouter" on nodeName.
func DeleteNamedNetns(nodeName string) error {
	exec := executor.ForContainer(nodeName)
	_, err := exec.Exec("ip", "netns", "delete", namedNetns)
	return err
}

// switchPortForNode maps Kind node names to their leafkind-switch port names
// as defined in the containerlab topology (clab/singlecluster/kind.clab.yml).
var switchPortForNode = map[string]string{
	"pe-kind-control-plane": "kindctrlpl",
	"pe-kind-worker":        "kindworker",
}

// underlayIPForNode maps Kind node names to their underlay IP/prefix on the
// leafkind-switch link, matching the containerlab topology.
var underlayIPForNode = map[string]string{
	"pe-kind-control-plane": "192.168.11.3/24",
	"pe-kind-worker":        "192.168.11.4/24",
}

// hostNetImage is a container image with iproute2, used for privileged host
// network operations. The FRR image is already cached by containerlab.
const hostNetImage = "quay.io/frrouting/frr:10.6.0"

// EnsureUnderlayLink checks whether the toswitch interface exists on nodeName
// (either in the default netns or inside the perouter netns) and recreates
// the veth pair if it is missing.
func EnsureUnderlayLink(nodeName string) error {
	nodeExec := executor.ForContainer(nodeName)

	if _, err := nodeExec.Exec("ip", "link", "show", "toswitch"); err == nil {
		return nil
	}

	if _, err := nodeExec.Exec("ip", "netns", "exec", namedNetns, "ip", "link", "show", "toswitch"); err == nil {
		return nil
	}

	return RecreateUnderlayLink(nodeName)
}

// RecreateUnderlayLink recreates the veth pair between a Kind node and the
// leafkind-switch bridge. This is needed after deleting the named netns
// because the kernel destroys veth pairs when a netns is destroyed — unlike
// physical NICs which are returned to the host netns.
//
// The containerlab topology uses a host-level Linux bridge ("leafkind-switch")
// with ports like "kindworker" and "kindctrlpl". The veth peer ("toswitch")
// lives inside the Kind node container. When the perouter netns is destroyed,
// "toswitch" is destroyed and the bridge-side port becomes a dead link.
//
// Host network operations require CAP_NET_ADMIN which the test runner lacks.
// We run them via a temporary privileged container with --net=host access.
func RecreateUnderlayLink(nodeName string) error {
	switchPort, ok := switchPortForNode[nodeName]
	if !ok {
		return fmt.Errorf("unknown node %s for underlay link recreation", nodeName)
	}
	underlayIP, ok := underlayIPForNode[nodeName]
	if !ok {
		return fmt.Errorf("unknown node %s for underlay IP assignment", nodeName)
	}
	const bridgeName = "leafkind-switch"

	nodeExec := executor.ForContainer(nodeName)

	pidOut, err := executor.Host.Exec("docker", "inspect", "-f", "{{.State.Pid}}", nodeName)
	if err != nil {
		return fmt.Errorf("failed to get PID for %s: %s: %w", nodeName, pidOut, err)
	}
	nodePid := strings.TrimSpace(pidOut)

	// Batch all host network operations into a single privileged container.
	script := fmt.Sprintf(`set -e
ip link delete %s 2>/dev/null || true
ip link delete toswitch-tmp 2>/dev/null || true
ip link add %s type veth peer name toswitch-tmp
ip link set %s master %s
ip link set %s up
ip link set toswitch-tmp netns %s
`, switchPort, switchPort, switchPort, bridgeName, switchPort, nodePid)

	out, err := executor.Host.Exec(executor.ContainerRuntime, "run", "--rm",
		"--net=host", "--pid=host", "--privileged",
		hostNetImage, "sh", "-c", script)
	if err != nil {
		return fmt.Errorf("failed to recreate underlay link on host: %s: %w", out, err)
	}

	// Rename, assign the underlay IP, and bring up inside the node container.
	if out, err := nodeExec.Exec("ip", "link", "set", "toswitch-tmp", "name", "toswitch"); err != nil {
		return fmt.Errorf("failed to rename toswitch in %s: %s: %w", nodeName, out, err)
	}
	if out, err := nodeExec.Exec("ip", "addr", "add", underlayIP, "dev", "toswitch"); err != nil {
		return fmt.Errorf("failed to assign underlay IP to toswitch on %s: %s: %w", nodeName, out, err)
	}
	if out, err := nodeExec.Exec("ip", "link", "set", "toswitch", "up"); err != nil {
		return fmt.Errorf("failed to bring up toswitch on %s: %s: %w", nodeName, out, err)
	}

	return nil
}
