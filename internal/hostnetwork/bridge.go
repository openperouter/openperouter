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
	name := BridgeName(params.VNI)
	bridge, err := createBridge(name, vrf.Index)
	if err != nil {
		return nil, err
	}

	err = setAddrGenModeNone(bridge)
	if err != nil {
		return nil, fmt.Errorf("failed to set addr_gen_mode to 1 for %s: %w", bridge.Name, err)
	}

	err = linkSetUp(bridge)
	if err != nil {
		return nil, fmt.Errorf("could not set link up for bridge %s: %v", name, err)
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

var macHeader = []byte{0x00, 0xF3}

// ensureBridgeFixedMacAddress sets a deterministic MAC address on the bridge based on the VNI.
// It is idempotent: if the MAC is already correct, it skips the update to avoid
// unnecessary RTM_NEWLINK events that can cause FRR to flush neighbor entries.
func ensureBridgeFixedMacAddress(bridge netlink.Link, vni int) error {
	macAddress := make([]byte, macSize)

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, int32(vni+1))
	if err != nil {
		return err
	}
	copy(macAddress, macHeader)
	copy(macAddress[2:], buf.Bytes())

	if bytes.Equal(bridge.Attrs().HardwareAddr, macAddress) {
		return nil
	}

	if err := netlink.LinkSetHardwareAddr(bridge, macAddress); err != nil {
		return fmt.Errorf("failed to set mac address to bridge %s %x: %w", bridge.Attrs().Name, macAddress, err)
	}
	return nil
}

const bridgePrefix = "br-pe-"

// BridgeName returns the PE bridge name for a given VNI.
func BridgeName(vni int) string {
	return fmt.Sprintf("%s%d", bridgePrefix, vni)
}

func vniFromBridgeName(name string) (int, error) {
	if !strings.HasPrefix(name, bridgePrefix) {
		return 0, NotRouterInterfaceError{Name: name}
	}

	vni := strings.TrimPrefix(name, bridgePrefix)
	res, err := strconv.Atoi(vni)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for bridge %s", name)
	}
	return res, nil
}
