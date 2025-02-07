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
	loopback, err := netlink.LinkByName(UnderlayLoopback)
	if err != nil {
		return fmt.Errorf("failed to get loopback by name: %w", err)
	}

	name := vxLanName(params.VNI)
	link, err := netlink.LinkByName(name)
	if err != nil && errors.As(err, &netlink.LinkNotFoundError{}) {
		link, err = createVXLan(params, bridge)
		if err != nil {
			return fmt.Errorf("failed to create vxlan %s: %w", name, err)
		}
	}
	vxlan, ok := link.(*netlink.Vxlan)
	if !ok {
		err := netlink.LinkDel(link)
		if err != nil {
			return fmt.Errorf("failed to delete link %v: %w", link, err)
		}
		vxlan, err = createVXLan(params, bridge)
		if err != nil {
			return fmt.Errorf("failed to create vxlan %s: %w", name, err)
		}
	}
	err = checkVXLanConfigured(vxlan, bridge.Index, loopback.Attrs().Index, params)
	if err != nil {
		// not configured: let's recreate it and configure it.
		err := netlink.LinkDel(link)
		if err != nil {
			return fmt.Errorf("failed to delete link %v: %w", link, err)
		}
		vxlan, err = createVXLan(params, bridge)
		if err != nil {
			return fmt.Errorf("failed to create vxlan %s: %w", name, err)
		}
	}

	err = setAddrGenModeNone(vxlan)
	if err != nil {
		return fmt.Errorf("failed to set addr_gen_mode to 1 for %s: %w", vxlan.Name, err)
	}
	err = setNeighSuppression(vxlan)
	if err != nil {
		return fmt.Errorf("failed to set neigh suppression for %s: %w", vxlan.Name, err)
	}

	err = netlink.LinkSetUp(vxlan)
	if err != nil {
		return fmt.Errorf("could not set link up for vxlan %s: %v", name, err)
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

	vtepIP, _, _ := net.ParseCIDR(params.VTEPIP) // TODO
	if !vxLan.SrcAddr.Equal(vtepIP) {
		return fmt.Errorf("src addr is not one coming from params: %v, %v", vxLan.SrcAddr, params.VTEPIP)
	}

	if vxLan.VtepDevIndex != loopbackIndex {
		return fmt.Errorf("vtep dev index is not loopback index: %d %d", vxLan.VtepDevIndex, loopbackIndex)
	}
	return nil
}

func createVXLan(params VNIParams, bridge *netlink.Bridge) (*netlink.Vxlan, error) {
	loopback, err := netlink.LinkByName(UnderlayLoopback)
	if err != nil {
		return nil, fmt.Errorf("failed to get loopback by name: %w", err)
	}

	name := vxLanName(params.VNI)

	vtepIP, _, _ := net.ParseCIDR(params.VTEPIP) // TODO
	vxlan := &netlink.Vxlan{LinkAttrs: netlink.LinkAttrs{
		Name:        name,
		MasterIndex: bridge.Index,
	},
		VxlanId:      params.VNI,
		Port:         params.VXLanPort,
		Learning:     false,
		SrcAddr:      vtepIP,
		VtepDevIndex: loopback.Attrs().Index,
	}
	err = netlink.LinkAdd(vxlan)
	if err != nil {
		return nil, fmt.Errorf("failed to create vxlan %s: %w", vxlan.Name, err)
	}
	return vxlan, nil
}

const vniPrefix = "vni"

func vxLanName(vni int) string {
	return fmt.Sprintf("%s%d", vniPrefix, vni)
}

func vniFromVXLanName(name string) (int, error) {
	vni := strings.TrimPrefix(name, vniPrefix)
	res, err := strconv.Atoi(vni)
	if err != nil {
		return 0, fmt.Errorf("failed to get vni for vxlan %s", name)
	}
	return res, nil
}
