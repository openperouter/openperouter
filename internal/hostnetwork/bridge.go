// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

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
	return ensureBridgeMACAddress(bridge, macAddress)
}

func ensureBridgeMACAddress(bridge netlink.Link, macAddress net.HardwareAddr) error {
	if bytes.Equal(bridge.Attrs().HardwareAddr, macAddress) {
		return nil
	}

	if err := netlink.LinkSetHardwareAddr(bridge, macAddress); err != nil {
		return fmt.Errorf("failed to set mac address to bridge %s %x: %w", bridge.Attrs().Name, macAddress, err)
	}
	bridge.Attrs().HardwareAddr = macAddress
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
