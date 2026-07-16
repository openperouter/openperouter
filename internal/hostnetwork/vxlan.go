// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

// setupVXLan sets up a vxlan interface corresponding to the provided
// vniParams.
func setupVXLan(params VNIParams, bridge *netlink.Bridge) error {
	vxlan, err := createVXLan(params, bridge)
	if err != nil {
		return err
	}

	if err := setAddrGenModeNone(vxlan); err != nil {
		return fmt.Errorf("failed to set addr_gen_mode to 1 for %s: %w", vxlan.Name, err)
	}
	if err := setNeighSuppression(vxlan); err != nil {
		return fmt.Errorf("failed to set neigh suppression for %s: %w", vxlan.Name, err)
	}

	if err = linkSetUp(vxlan); err != nil {
		return fmt.Errorf("could not set link up for vxlan %s: %v", vxlan.Name, err)
	}

	return nil
}

// checkVXLanConfigured checks if the given VXLan has the required properties
// passed as parameters.
func checkVXLanConfigured(vxLan *netlink.Vxlan, bridgeIndex, loopbackIndex int, params VNIParams) error {
	if vxLan.MasterIndex != bridgeIndex {
		return fmt.Errorf("master index is not bridge index: %d, %d", vxLan.MasterIndex, bridgeIndex)
	}

	if vxLan.VxlanId != int(params.VNI) {
		return fmt.Errorf("vxlanid is not vni: %d, %d", vxLan.VxlanId, params.VNI)
	}

	if params.VXLanPort == nil {
		return errors.New("failed to parse VXLAN information, VXLAN port is nil")
	}

	paramsVXLanPort := int(*params.VXLanPort)
	if vxLan.Port != paramsVXLanPort {
		return fmt.Errorf("port is not one coming from params: %d, %d", vxLan.Port, paramsVXLanPort)
	}
	if vxLan.Learning {
		return fmt.Errorf("learning is enabled")
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
			Name:        vxlanName,
			MasterIndex: bridge.Index,
		},
		VxlanId:      int(params.VNI),
		Port:         int(*params.VXLanPort),
		Learning:     false,
		VtepDevIndex: loopback.Index,
		SrcAddr:      vtepIP,
	}

	link, err := netlink.LinkByName(vxlanName)
	if err != nil && errors.As(err, &netlink.LinkNotFoundError{}) {
		if err := netlink.LinkAdd(toCreate); err != nil {
			return nil, fmt.Errorf("failed to create vxlan %s: %w", vxlanName, err)
		}
		return toCreate, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get vxlan link by name %s: %w", vxlanName, err)
	}
	vxlan, ok := link.(*netlink.Vxlan)
	if ok && checkVXLanConfigured(vxlan, bridge.Index, loopback.Index, params) == nil {
		return vxlan, nil
	}
	if err := netlink.LinkDel(link); err != nil {
		return nil, fmt.Errorf("failed to delete link %v: %w", link, err)
	}

	if err = netlink.LinkAdd(toCreate); err != nil {
		return nil, fmt.Errorf("failed to create vxlan %s: %w", vxlan.Name, err)
	}
	return toCreate, nil
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
