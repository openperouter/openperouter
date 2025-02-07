// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

// setup bridge creates the bridge if not exists, and it enslaves it to the provided
// vrf.
func setupBridge(params VNIParams, vrf *netlink.Vrf) (*netlink.Bridge, error) {
	name := bridgeName(params.VNI)
	link, err := netlink.LinkByName(name)
	// link does not exist, let's create it
	if err != nil && errors.As(err, &netlink.LinkNotFoundError{}) {
		link, err = createBridge(name, vrf.Index)
		if err != nil {
			return nil, fmt.Errorf("failed to create bridge %s: %w", name, err)
		}
	}

	bridge, ok := link.(*netlink.Bridge)
	if !ok {
		// link exists but it's not a bridge, let's delete and create
		err := netlink.LinkDel(link)
		if err != nil {
			return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
		}
		bridge, err = createBridge(name, vrf.Index)
		if err != nil {
			return nil, fmt.Errorf("failed to create bridge %s: %w", name, err)
		}
	}

	err = setAddrGenModeNone(bridge)
	if err != nil {
		return nil, fmt.Errorf("failed to set addr_gen_mode to 1 for %s: %w", bridge.Name, err)
	}

	err = netlink.LinkSetUp(bridge)
	if err != nil {
		return nil, fmt.Errorf("could not set link up for bridge %s: %v", name, err)
	}
	return bridge, nil
}

// create bridge creates a bridge with the given name, enslaved
// to the provided vrf.
func createBridge(name string, vrfIndex int) (*netlink.Bridge, error) {
	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{
		Name:        name,
		MasterIndex: vrfIndex,
	}}
	err := netlink.LinkAdd(bridge)
	if err != nil {
		return nil, fmt.Errorf("could not create bridge %s", name)
	}

	return bridge, nil
}

const bridgePrefix = "br"

func bridgeName(vni int) string {
	return fmt.Sprintf("%s%d", bridgePrefix, vni)
}

func vniFromBridgeName(name string) (int, error) {
	vni := strings.TrimPrefix(name, bridgePrefix)
	res, err := strconv.Atoi(vni)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for bridge %s", name)
	}
	return res, nil
}
