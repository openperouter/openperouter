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

	if vxLan.VxlanId != params.VNI {
		return fmt.Errorf("vxlanid is not vni: %d, %d", vxLan.VxlanId, params.VNI)
	}

	if vxLan.Port != params.VXLanPort {
		return fmt.Errorf("port is not one coming from params: %d, %d", vxLan.Port, params.VXLanPort)
	}
	if vxLan.Learning {
		return fmt.Errorf("learning is enabled")
	}
	if err := checkVxlanSrcAddr(vxLan, params); err != nil {
		return err
	}
	if vxLan.VtepDevIndex != loopbackIndex {
		return fmt.Errorf("vtep dev index is not loopback index: %d %d", vxLan.VtepDevIndex, loopbackIndex)
	}
	return nil
}

func createVXLan(params VNIParams, bridge *netlink.Bridge) (*netlink.Vxlan, error) {
	vtepInterfaceName := UnderlayLoopback
	if params.VTEPInterface != "" {
		vtepInterfaceName = params.VTEPInterface
	}
	vtepInterface, err := net.InterfaceByName(vtepInterfaceName)
	if err != nil {
		return nil, fmt.Errorf("failed looking for vtep interface %s: %w", vtepInterfaceName, err)
	}

	srcAddr, err := findVxlanSrcAddr(vtepInterface, params)
	if err != nil {
		return nil, err
	}

	vxlanName := vxLanNameFromVNI(params.VNI)
	toCreate := &netlink.Vxlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        vxlanName,
			MasterIndex: bridge.Index,
		},
		VxlanId:      params.VNI,
		Port:         params.VXLanPort,
		Learning:     false,
		VtepDevIndex: vtepInterface.Index,
		SrcAddr:      srcAddr,
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
	if ok && checkVXLanConfigured(vxlan, bridge.Index, vtepInterface.Index, params) == nil {
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

func vxLanNameFromVNI(vni int) string {
	return fmt.Sprintf("%s%d", vniPrefix, vni)
}

func vniFromVXLanName(name string) (int, error) {
	if !strings.HasPrefix(name, vniPrefix) {
		return 0, NotRouterInterfaceError{Name: name}
	}
	vni := strings.TrimPrefix(name, vniPrefix)
	res, err := strconv.Atoi(vni)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for vxlan %s", name)
	}
	return res, nil
}

// findFirstInterfaceIPv4Address returns the first IPv4 address assigned to the interface,
// skipping the special underlay marker address.
func findFirstInterfaceIPv4Address(iface *net.Interface) (net.IP, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("failed to get addresses: %w", err)
	}
	for _, addr := range addrs {
		if addr.String() == underlayInterfaceSpecialAddr {
			continue
		}
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.To4() == nil {
			continue
		}
		return ip, nil
	}
	return nil, fmt.Errorf("no IPv4 address found on interface %s", iface.Name)
}

func checkVxlanSrcAddr(vxLan *netlink.Vxlan, params VNIParams) error {
	if params.VTEPIP != "" {
		vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
		if err != nil {
			return fmt.Errorf("failed to parse vtep ip %v: %w", params.VTEPIP, err)
		}
		if !vxLan.SrcAddr.Equal(vtepIP) {
			return fmt.Errorf("src addr is not one coming from params: %v, %v", vxLan.SrcAddr, params.VTEPIP)
		}
		return nil
	}

	if params.VTEPInterface != "" {
		iface, err := net.InterfaceByName(params.VTEPInterface)
		if err != nil {
			return fmt.Errorf("failed to get vtep interface %s: %w", params.VTEPInterface, err)
		}
		expectedIP, err := findFirstInterfaceIPv4Address(iface)
		if err != nil {
			return fmt.Errorf("failed to get vtep source address from interface %s: %w", params.VTEPInterface, err)
		}
		if !vxLan.SrcAddr.Equal(expectedIP) {
			return fmt.Errorf("src addr does not match vtep interface ip: %v, expected %v", vxLan.SrcAddr, expectedIP)
		}
		return nil
	}

	return fmt.Errorf("missing vtepip or vtepinterface")
}

// findVxlanSrcAddr resolve the source IP for the VXLAN interface.
// When VTEPIP is explicitly provided (vtepCIDR mode), use it directly.
// When using VTEPInterface, resolve the IP from the interface itself.
func findVxlanSrcAddr(vtepInterface *net.Interface, params VNIParams) (net.IP, error) {
	if params.VTEPIP != "" {
		vtepIP, _, err := net.ParseCIDR(params.VTEPIP)
		if err != nil {
			return nil, fmt.Errorf("failed to parse vtep ip %v: %w", params.VTEPIP, err)
		}
		return vtepIP, nil
	}

	if params.VTEPInterface != "" {
		srcAddr, err := findFirstInterfaceIPv4Address(vtepInterface)
		if err != nil {
			return nil, fmt.Errorf("failed to get vtep source address from interface %s: %w", vtepInterface.Name, err)
		}
		return srcAddr, nil
	}

	return nil, fmt.Errorf("missing vtepip or vtepinterface")
}
