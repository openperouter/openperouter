// SPDX-License-Identifier:Apache-2.0

package bridgerefresh

import (
	"fmt"
	"net"
	"os/exec"
)

// sendPing sends an ICMP ping to the target IP via the bridge interface.
// This refreshes the neighbor entry, preventing EVPN Type-2 route withdrawal and
// also ensures stale routes are garbage collected.
func (r *BridgeRefresher) sendPing(targetIP net.IP) error {
	out, err := exec.Command("ping", "-c", "1", "-W", "1", "-I", r.bridgeName, targetIP.String()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ping to %s via %s failed: %w (output: %s)", targetIP, r.bridgeName, err, out)
	}
	return nil
}
