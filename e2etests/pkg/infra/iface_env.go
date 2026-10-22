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

// UnderlayIfacesConfig is the resolved underlay host iface names and dual-leaf flag.
type UnderlayIfacesConfig struct {
	Iface1   string
	Iface2   string
	DualLeaf bool
}

// ResolveUnderlayHostIfaces builds config from nodelink underlayHostIfaces.
// Empty → Kind defaults; one name → single-leaf; two names → dual-leaf.
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

// Ifaces returns the active underlay host iface names.
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

// ConfigureUnderlayIfaces applies cfg to underlay fixtures and netns checks.
func ConfigureUnderlayIfaces(cfg UnderlayIfacesConfig) {
	underlayIfaces = cfg
	applyUnderlayInterfaces(cfg)
	openperouter.SetUnderlayHostIfaces(cfg.Ifaces())
}

// UnderlayIface1 returns the primary underlay host iface name.
func UnderlayIface1() string {
	return underlayIfaces.Iface1
}

// UnderlayIface2 returns the secondary underlay host iface name.
func UnderlayIface2() string {
	return underlayIfaces.Iface2
}

// UnderlayIfaces returns the active underlay host iface names.
func UnderlayIfaces() []string {
	return underlayIfaces.Ifaces()
}

// DualLeafEnabled reports whether the second TOR path is active.
func DualLeafEnabled() bool {
	return underlayIfaces.DualLeaf
}
