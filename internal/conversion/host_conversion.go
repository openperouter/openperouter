// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
)

func APItoHostConfig(nodeIndex int, targetNS string, underlays []v1alpha1.Underlay, vnis []v1alpha1.L3VNI, l2vnis []v1alpha1.L2VNI) (hostnetwork.LoopbackParams, hostnetwork.NICParams, []hostnetwork.L3VNIParams, []hostnetwork.L2VNIParams, error) {
	if len(underlays) > 1 {
		return hostnetwork.LoopbackParams{}, hostnetwork.NICParams{}, nil, nil, fmt.Errorf("can't have more than one underlay")
	}
	if len(underlays) == 0 {
		return hostnetwork.LoopbackParams{}, hostnetwork.NICParams{}, nil, nil, nil
	}

	underlay := underlays[0]

	vtepIP, err := ipam.VTEPIp(underlay.Spec.VTEPCIDR, nodeIndex)
	if err != nil {
		return hostnetwork.LoopbackParams{}, hostnetwork.NICParams{}, nil, nil, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIntex %d", underlay.Spec.VTEPCIDR, nodeIndex)
	}

	loopbackParams := hostnetwork.LoopbackParams{
		VtepIP:   vtepIP.String(),
		TargetNS: targetNS,
	}

	nicParams := hostnetwork.NICParams{
		UnderlayInterface: underlay.Spec.Nics[0],
		TargetNS:          targetNS,
	}

	l3vniParams := []hostnetwork.L3VNIParams{}

	for _, vni := range vnis {
		vethIPs, err := ipam.VethIPsFromPool(vni.Spec.LocalCIDR.IPv4, vni.Spec.LocalCIDR.IPv6, nodeIndex)
		if err != nil {
			return hostnetwork.LoopbackParams{}, hostnetwork.NICParams{}, nil, nil, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d", vni.Spec.LocalCIDR, nodeIndex)
		}

		v := hostnetwork.L3VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:       vni.VRFName(),
				TargetNS:  targetNS,
				VTEPIP:    vtepIP.String(),
				VNI:       int(vni.Spec.VNI),
				VXLanPort: int(vni.Spec.VXLanPort),
			},
			VethHostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
			VethNSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
			VethHostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
			VethNSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
		}
		l3vniParams = append(l3vniParams, v)
	}

	l2vniParams := []hostnetwork.L2VNIParams{}
	for _, l2vni := range l2vnis {
		vni := hostnetwork.L2VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:       l2vni.VRFName(),
				TargetNS:  targetNS,
				VTEPIP:    vtepIP.String(),
				VNI:       int(l2vni.Spec.VNI),
				VXLanPort: int(l2vni.Spec.VXLanPort),
			},
		}
		if l2vni.Spec.L2GatewayIP != "" {
			vni.L2GatewayIP = &l2vni.Spec.L2GatewayIP
		}
		if l2vni.Spec.HostMaster != nil {
			vni.HostMaster = &hostnetwork.HostMaster{
				Name:       l2vni.Spec.HostMaster.Name,
				AutoCreate: l2vni.Spec.HostMaster.AutoCreate,
			}
		}

		l2vniParams = append(l2vniParams, vni)
	}

	return loopbackParams, nicParams, l3vniParams, l2vniParams, nil
}

// ipNetToString returns the string representation of the IPNet, or empty string if IP is nil
func ipNetToString(ipNet net.IPNet) string {
	if ipNet.IP == nil {
		return ""
	}
	return ipNet.String()
}
