// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
)

const (
	defaultUnderlayIface  = "toswitch1"
	defaultUnderlayIface2 = "toswitch2"
)

// UnderlayIfacesConfig holds resolved underlay host interface names and
// whether dual-leaf (second TOR) peering is enabled.
//
// DualLeaf gates inclusion of Iface2 in underlay fixtures and host-iface
// checks. When DualLeaf is false, Iface2 is unused even if it still holds
// the kind default name (toswitch2).
type UnderlayIfacesConfig struct {
	Iface1   string
	Iface2   string
	DualLeaf bool
}

// ResolveUnderlayHostIfaces builds an UnderlayIfacesConfig from the
// underlayHostIfaces list in nodelink JSON (see RegisterLinks).
//
//   - nil or empty: Kind defaults (toswitch1/toswitch2, DualLeaf enabled)
//   - one name: DualLeaf disabled (single-uplink baremetal)
//   - two names: DualLeaf enabled
//   - more than two: error
func ResolveUnderlayHostIfaces(ifaces []string) (UnderlayIfacesConfig, error) {
	switch len(ifaces) {
	case 0:
		return UnderlayIfacesConfig{
			Iface1:   defaultUnderlayIface,
			Iface2:   defaultUnderlayIface2,
			DualLeaf: true,
		}, nil
	case 1:
		if ifaces[0] == "" {
			return UnderlayIfacesConfig{}, fmt.Errorf("underlayHostIfaces[0] is empty")
		}
		return UnderlayIfacesConfig{
			Iface1:   ifaces[0],
			Iface2:   defaultUnderlayIface2,
			DualLeaf: false,
		}, nil
	case 2:
		if ifaces[0] == "" || ifaces[1] == "" {
			return UnderlayIfacesConfig{}, fmt.Errorf("underlayHostIfaces entries must be non-empty")
		}
		return UnderlayIfacesConfig{
			Iface1:   ifaces[0],
			Iface2:   ifaces[1],
			DualLeaf: true,
		}, nil
	default:
		return UnderlayIfacesConfig{}, fmt.Errorf("underlayHostIfaces: want 0-2 names, got %d", len(ifaces))
	}
}

// Ifaces returns the underlay host interface names that are in use.
// When DualLeaf is false, only the primary interface is returned.
func (c UnderlayIfacesConfig) Ifaces() []string {
	if c.DualLeaf {
		return []string{c.Iface1, c.Iface2}
	}
	return []string{c.Iface1}
}

var underlayIfaces = UnderlayIfacesConfig{
	Iface1:   defaultUnderlayIface,
	Iface2:   defaultUnderlayIface2,
	DualLeaf: true,
}

// ConfigureUnderlayIfaces installs the resolved underlay interface config used
// by fixtures and helpers for the rest of the process, including the host
// iface names checked by openperouter.UnderlayVethsExists. RegisterLinks
// calls this from the underlayHostIfaces field in nodelink JSON.
func ConfigureUnderlayIfaces(cfg UnderlayIfacesConfig) {
	underlayIfaces = cfg
	applyUnderlayInterfaces(cfg)
	openperouter.SetUnderlayHostIfaces(cfg.Ifaces())
}

// UnderlayIface1 returns the configured primary underlay host interface name.
func UnderlayIface1() string {
	return underlayIfaces.Iface1
}

// UnderlayIface2 returns the configured secondary underlay host interface name.
// Meaningful only when DualLeafEnabled is true; fixtures omit it otherwise.
func UnderlayIface2() string {
	return underlayIfaces.Iface2
}

// UnderlayIfaces returns the underlay host interface names currently in use.
func UnderlayIfaces() []string {
	return underlayIfaces.Ifaces()
}

// DualLeafEnabled reports whether the second TOR path is active. Kind defaults
// to enabled; a single entry in underlayHostIfaces disables it.
func DualLeafEnabled() bool {
	return underlayIfaces.DualLeaf
}
