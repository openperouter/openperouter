// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"strings"
)

// ResetISIS removes the ISIS configuration from every router's FRR runtime state.
//
// It works around an frr-reload.py ordering bug hit when an ISIS underlay is
// removed: frr-reload emits "no isis passive" on the loopback after it has
// already detached the interface from the ISIS instance, and FRR rejects that
// with a YANG "area-tag" error. The failed reload leaves isisd holding the stale
// "isis passive" line, which then poisons the next reload against an ISIS-free
// config.
//
// Running the removal here in the correct order - clear the interface-level
// passive setting while the ISIS instance still exists, then remove the instance
// - leaves isisd clean. See issue #645.
func ResetISIS(routers Routers, processName string) error {
	for router := range routers.GetExecutors() {
		running, err := router.Exec("vtysh", "-c", "show running-config")
		if err != nil {
			return fmt.Errorf("failed to read running config on router %s: %w", router.Name(), err)
		}
		if !strings.Contains(running, "router isis") {
			continue
		}

		out, err := router.Exec("vtysh",
			"-c", "configure terminal",
			"-c", "interface lo",
			"-c", "no isis passive",
			"-c", "exit",
			"-c", fmt.Sprintf("no router isis %s", processName),
			"-c", "end",
		)
		if err != nil {
			return fmt.Errorf("failed to reset ISIS on router %s: %w, output: %s", router.Name(), err, out)
		}
	}
	return nil
}
