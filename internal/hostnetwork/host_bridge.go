// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

// createHostBridge creates a bridge on the host namespace named after the
// provided vni. If the bridge already exists, it will return the existing one.
func createHostBridge(vni int32) (netlink.Link, error) {
	name := hostBridgeName(vni)
	_, err := netlink.LinkByName(name)
	// link does not exist, let's create it
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		toCreate := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: name}}
		if err := netlink.LinkAdd(toCreate); err != nil {
			return nil, fmt.Errorf("could not create host bridge %s: %w", name, err)
		}
	}
	bridge, err := netlink.LinkByName(name)
	if err != nil {
		return nil, fmt.Errorf("could not find host bridge %s: %w", name, err)
	}
	if err := linkSetUp(bridge); err != nil {
		return nil, fmt.Errorf("could not set host bridge %s up: %w", name, err)
	}

	slog.Debug("created host bridge", "name", name)
	return bridge, nil
}

const hostBridgePrefix = "br-hs-"

func hostBridgeName(vni int32) string {
	return fmt.Sprintf("%s%d", hostBridgePrefix, vni)
}

// interfaceIDFromHostBridgeName extracts the interface ID from a host bridge name.
func interfaceIDFromHostBridgeName(name string) (int32, error) {
	if !strings.HasPrefix(name, hostBridgePrefix) {
		return 0, NotRouterInterfaceError{Name: name}
	}
	vni := strings.TrimPrefix(name, hostBridgePrefix)
	res, err := strconv.ParseInt(vni, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for host bridge %s: %w", name, err)
	}
	return int32(res), nil
}
