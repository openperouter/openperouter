// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

// setupVXLan sets up a vxlan interface corresponding to the provided
// vniParams.
func setupVXLan(params VNIParams, bridge *netlink.Bridge) error {
	vxlan, err := createVXLan(params, bridge)
	if err != nil {
		return err
	}

	if err := ensureAddressless(vxlan); err != nil {
		return err
	}
	if err := linkSetMaster(vxlan, bridge); err != nil {
		return fmt.Errorf("attaching VXLAN %s to bridge %s: %w", vxlan.Name, bridge.Name, err)
	}
	port, err := netlink.LinkGetProtinfo(vxlan)
	if err != nil {
		return fmt.Errorf("reading bridge port state for %s: %w", vxlan.Name, err)
	}
	if !port.NeighSuppress {
		if err := netlink.LinkSetBrNeighSuppress(vxlan, true); err != nil {
			return fmt.Errorf("failed to set neigh suppression for %s: %w", vxlan.Name, err)
		}
	}
	// LinkSetLearning disables MAC learning for the bridge_slave. ip link syntax is:
	// ip link set <...> type bridge_slave learning off
	// Note that this is different from the VXLAN's 'nolearning' parameter (which we set inside createVXLan via
	// Learning: false) and which disables VTEP learning.
	if port.Learning {
		if err := netlink.LinkSetLearning(vxlan, false); err != nil {
			return fmt.Errorf("failed to disable MAC learning (learning off) for %s: %w", vxlan.Name, err)
		}
	}
	if !port.VlanTunnel {
		if err := netlink.LinkSetVlanTunnel(vxlan, true); err != nil {
			return fmt.Errorf("enabling VLAN tunnel mode for %s: %w", vxlan.Name, err)
		}
	}
	if err := ensureVLANMembership(vxlan, false, false); err != nil {
		return err
	}
	if err := ensureVXLANTunnelMapping(vxlan, uint32(params.VNI)); err != nil {
		return err
	}
	if err := ensureVNIFilter(vxlan.Index, uint32(params.VNI)); err != nil {
		return err
	}
	if !bytes.Equal(vxlan.HardwareAddr, bridge.HardwareAddr) {
		if err := netlink.LinkSetHardwareAddr(vxlan, bridge.HardwareAddr); err != nil {
			return fmt.Errorf("setting VXLAN %s router MAC: %w", vxlan.Name, err)
		}
	}

	if err = linkSetUp(vxlan); err != nil {
		return fmt.Errorf("could not set link up for vxlan %s: %v", vxlan.Name, err)
	}

	return nil
}

// checkVXLanConfigured compares the device attributes that require replacement.
// Bridge membership and bridge port state are reconciled independently.
func checkVXLanConfigured(vxLan *netlink.Vxlan, loopbackIndex int, params VNIParams) error {
	if !vxLan.FlowBased || vxLan.VxlanId != 0 {
		return errors.New("VXLAN is not an external metadata device")
	}

	if params.VXLanPort == nil {
		return errors.New("failed to parse VXLAN information, VXLAN port is nil")
	}

	paramsVXLanPort := int(*params.VXLanPort)
	if vxLan.Port != paramsVXLanPort {
		return fmt.Errorf("port is not one coming from params: %d, %d", vxLan.Port, paramsVXLanPort)
	}
	if vxLan.Learning {
		return errors.New("VTEP learning is enabled")
	}
	if err := validateVxlan(vxLan, params); err != nil {
		return err
	}
	if vxLan.VtepDevIndex != loopbackIndex {
		return fmt.Errorf("vtep dev index is not loopback index: %d %d", vxLan.VtepDevIndex, loopbackIndex)
	}
	return nil
}

func createVXLan(params VNIParams, bridge *netlink.Bridge) (*netlink.Vxlan, error) {
	loopback, err := net.InterfaceByName(loopbackName)
	if err != nil {
		return nil, fmt.Errorf("failed looking for vtep loopback interface %s: %w", loopbackName, err)
	}

	vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
	if err != nil {
		return nil, fmt.Errorf("failed to parse vtep ip %v: %w", params.VTEPIP, err)
	}

	if params.VXLanPort == nil {
		return nil, errors.New("failed to parse VXLAN information, VXLAN port is nil")
	}

	vxlanName := vxLanNameFromVNI(params.VNI)
	toCreate := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:         vxlanName,
			MasterIndex:  bridge.Index,
			HardwareAddr: bridge.HardwareAddr,
		},
		FlowBased:    true,
		Port:         int(*params.VXLanPort),
		Learning:     false, // Disable VTEP learning (ip link add <...> type vxlan <...> nolearning).
		VtepDevIndex: loopback.Index,
		SrcAddr:      vtepIP,
	}

	link, err := netlink.LinkByName(vxlanName)
	if err != nil && errors.As(err, &netlink.LinkNotFoundError{}) {
		return createFilteredVXLAN(toCreate)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get vxlan link by name %s: %w", vxlanName, err)
	}
	vxlan, ok := link.(*netlink.Vxlan)
	if ok && checkVXLanConfigured(vxlan, loopback.Index, params) == nil {
		enabled, err := vxlanVNIFilterEnabled(vxlan.Index)
		if err != nil {
			return nil, err
		}
		if enabled {
			return vxlan, nil
		}
	}
	if err := netlink.LinkDel(link); err != nil {
		return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
	}

	return createFilteredVXLAN(toCreate)
}

func ensureVXLANTunnelMapping(vxlan *netlink.Vxlan, vni uint32) error {
	mappings, err := vxlanTunnelMappings(vxlan.Index)
	if err != nil {
		return err
	}
	found := false
	for _, mapping := range mappings {
		if mapping == (nl.TunnelInfo{Vid: internalVLANID, TunId: vni}) {
			found = true
			continue
		}
		if err := netlink.BridgeVlanDelTunnelInfo(vxlan, mapping.Vid, mapping.TunId, false, false); err != nil {
			return fmt.Errorf("deleting VXLAN %s VLAN %d tunnel mapping: %w", vxlan.Name, mapping.Vid, err)
		}
	}
	if found {
		return nil
	}
	if err := netlink.BridgeVlanAddTunnelInfo(vxlan, internalVLANID, vni, false, false); err != nil {
		return fmt.Errorf("adding VXLAN %s internal VLAN tunnel mapping: %w", vxlan.Name, err)
	}
	return nil
}

const vniPrefix = "vni"

func vxLanNameFromVNI(vni int32) string {
	return fmt.Sprintf("%s%d", vniPrefix, vni)
}

func vniFromVXLanName(name string) (int32, error) {
	if !strings.HasPrefix(name, vniPrefix) {
		return 0, NotRouterInterfaceError{Name: name}
	}
	vni := strings.TrimPrefix(name, vniPrefix)
	res, err := strconv.ParseInt(vni, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for vxlan %s", name)
	}
	return int32(res), nil
}

func validateVxlan(vxLan *netlink.Vxlan, params VNIParams) error {
	vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
	if err != nil {
		return fmt.Errorf("failed to parse vtep ip %v: %w", params.VTEPIP, err)
	}
	if !vxLan.SrcAddr.Equal(vtepIP) {
		return fmt.Errorf("src addr does not match vtep ip: %v, expected %v", vxLan.SrcAddr, vtepIP)
	}
	return nil
}
