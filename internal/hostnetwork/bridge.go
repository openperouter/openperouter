// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

func setupBridge(params VNIParams) (*netlink.Bridge, error) {
	bridge, err := createVLANBridge(vlanBridgeName(params.VNI))
	if err != nil {
		return nil, err
	}
	if bridge.MasterIndex != 0 {
		if err := netlink.LinkSetNoMaster(bridge); err != nil {
			return nil, fmt.Errorf("detach switching bridge %s from master: %w", bridge.Name, err)
		}
		bridge.MasterIndex = 0
	}
	if bridge.VlanFiltering == nil || !*bridge.VlanFiltering {
		if err := netlink.BridgeSetVlanFiltering(bridge, true); err != nil {
			return nil, fmt.Errorf("enable VLAN filtering on %s: %w", bridge.Name, err)
		}
		bridge.VlanFiltering = new(true)
	}
	if bridge.VlanDefaultPVID == nil || *bridge.VlanDefaultPVID != 0 {
		if err := netlink.BridgeSetVlanDefaultPVID(bridge, 0); err != nil {
			return nil, fmt.Errorf("disable default PVID on %s: %w", bridge.Name, err)
		}
		bridge.VlanDefaultPVID = new(uint16(0))
	}
	if err := ensureVLANMembership(bridge, false, true); err != nil {
		return nil, err
	}
	if err := ensureAddressless(bridge); err != nil {
		return nil, err
	}
	if err := linkSetUp(bridge); err != nil {
		return nil, fmt.Errorf("could not set link up for bridge %s: %w", bridge.Name, err)
	}
	return bridge, nil
}

const (
	macSize = 6
)

var macHeader = []byte{0x00, 0xF3}

// BridgeFixedMAC returns the deterministic anycast MAC address for a given VNI.
// All nodes must use the same MAC for distributed L2 gateways.
func BridgeFixedMAC(vni int32) ([]byte, error) {
	macAddress := make([]byte, macSize)
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, vni+1)
	if err != nil {
		return nil, err
	}
	copy(macAddress, macHeader)
	copy(macAddress[2:], buf.Bytes())
	return macAddress, nil
}

// ensureBridgeFixedMacAddress sets a deterministic MAC address on the bridge based on the VNI.
// It is idempotent: if the MAC is already correct, it skips the update to avoid
// unnecessary RTM_NEWLINK events that can cause FRR to flush neighbor entries.
func ensureBridgeFixedMacAddress(bridge netlink.Link, vni int32) error {
	macAddress, err := BridgeFixedMAC(vni)
	if err != nil {
		return err
	}

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
func BridgeName(vni int32) string {
	return fmt.Sprintf("%s%d", bridgePrefix, vni)
}

func vniFromBridgeName(name string) (int32, error) {
	if !strings.HasPrefix(name, bridgePrefix) {
		return 0, NotRouterInterfaceError{Name: name}
	}

	vni := strings.TrimPrefix(name, bridgePrefix)
	res, err := strconv.ParseInt(vni, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for bridge %s", name)
	}
	return int32(res), nil
}
