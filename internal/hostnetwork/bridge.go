// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"encoding/binary"
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
	bridge, err := createBridge(name, vrf.Index)
	if err != nil {
		return nil, err
	}

	err = setAddrGenModeNone(bridge)
	if err != nil {
		return nil, fmt.Errorf("failed to set addr_gen_mode to 1 for %s: %w", bridge.Name, err)
	}

	err = netlink.LinkSetUp(bridge)
	if err != nil {
		return nil, fmt.Errorf("could not set link up for bridge %s: %v", name, err)
	}
	if params.Type == L3 {
		return bridge, nil
	}
	// setting up the same mac address for all the nodes for distributed gateway
	if err := setBridgeMacAddress(bridge, params.VNI); err != nil {
		return nil, fmt.Errorf("failed to set bridge mac address %s: %v", name, err)
	}
	return bridge, nil

}

// create bridge creates a bridge with the given name, enslaved
// to the provided vrf.
func createBridge(name string, vrfIndex int) (*netlink.Bridge, error) {

	toCreate := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{
		Name:        name,
		MasterIndex: vrfIndex,
	}}

	link, err := netlink.LinkByName(name)
	// link does not exist, let's create it
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		if err := netlink.LinkAdd(toCreate); err != nil {
			return nil, fmt.Errorf("could not create bridge %s: %w", name, err)
		}
		return toCreate, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could find bridge by name %s: %w", name, err)
	}

	bridge, ok := link.(*netlink.Bridge)
	if ok && bridge.MasterIndex == vrfIndex { // link exists, nothing to do here
		return bridge, nil
	}

	// link exists but it's not a bridge, or wrong vrf. Let's delete and create
	err = netlink.LinkDel(link)
	if err != nil {
		return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
	}
	if err := netlink.LinkAdd(toCreate); err != nil {
		return nil, fmt.Errorf("could not create bridge %s: %w", name, err)
	}

	return toCreate, nil
}

const (
	macSize = 6
)

var macHeader = []byte{0xF3, 0xD3}

func setBridgeMacAddress(bridge netlink.Link, vni int) error {
	macAddress := make([]byte, macSize)

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, int32(vni))
	if err != nil {
		return err
	}
	copy(macAddress, macHeader)
	copy(macAddress[2:], buf.Bytes())
	if err := netlink.LinkSetHardwareAddr(bridge, macAddress); err != nil {
		return fmt.Errorf("failed to set mac address to bridge %s %s: %w", bridge.Attrs().Name, macAddress, err)
	}
	return nil
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
