// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func APItoHostConfig(nodeIndex int, targetNS string, underlayFromMultus bool, apiConfig ApiConfigData) (HostConfigData, error) {
	res := HostConfigData{
		L3VNIs: []hostnetwork.L3VNIParams{},
		L2VNIs: []hostnetwork.L2VNIParams{},
	}
	if len(apiConfig.Underlays) > 1 {
		return res, fmt.Errorf("can't have more than one underlay")
	}
	if len(apiConfig.L3Passthrough) > 1 {
		return res, fmt.Errorf("can't have more than one passthrough")
	}
	if len(apiConfig.Underlays) == 0 {
		return res, nil
	}

	underlay := apiConfig.Underlays[0]

	if len(underlay.Spec.Nics) == 0 && !underlayFromMultus {
		return res, fmt.Errorf("underlay interface must be specified when Multus is not enabled")
	}

	nicMode := string(underlay.Spec.NicMode)
	if nicMode == "" {
		nicMode = "dedicated"
	}

	neighborIPs := make([]string, 0, len(underlay.Spec.Neighbors))
	for _, n := range underlay.Spec.Neighbors {
		if n.Address != "" {
			neighborIPs = append(neighborIPs, n.Address)
		}
	}

	res.Underlay = hostnetwork.UnderlayParams{
		TargetNS:    targetNS,
		NicMode:     nicMode,
		NeighborIPs: neighborIPs,
	}
	if len(underlay.Spec.Nics) > 0 {
		res.Underlay.UnderlayInterface = underlay.Spec.Nics[0]
	}

	if len(apiConfig.L3Passthrough) == 1 {
		vethIPs, err := ipam.VethIPsFromPool(apiConfig.L3Passthrough[0].Spec.HostSession.LocalCIDR.IPv4, apiConfig.L3Passthrough[0].Spec.HostSession.LocalCIDR.IPv6, nodeIndex)
		if err != nil {
			return res, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d", apiConfig.L3Passthrough[0].Spec.HostSession.LocalCIDR, nodeIndex)
		}

		res.L3Passthrough = &hostnetwork.PassthroughParams{
			TargetNS: targetNS,
			HostVeth: hostnetwork.Veth{
				HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
				NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
				HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
				NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
			},
		}
	}

	// EVPN is required when VNIs are defined, but EVPN without VNIs is allowed
	// (e.g., for preparation or advanced BGP EVPN use cases)
	if underlay.Spec.EVPN == nil && (len(apiConfig.L3VNIs) > 0 || len(apiConfig.L2VNIs) > 0) {
		return res, fmt.Errorf("underlay EVPN configuration is required when L3 or L2 VNIs are defined")
	}

	if underlay.Spec.EVPN == nil {
		return res, nil
	}

	res.Underlay.EVPN = &hostnetwork.UnderlayEVPNParams{}
	if underlay.Spec.EVPN.VTEPCIDR != "" {
		vtepIP, err := ipam.VTEPIp(underlay.Spec.EVPN.VTEPCIDR, nodeIndex)
		if err != nil {
			return res, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w", underlay.Spec.EVPN.VTEPCIDR, nodeIndex, err)
		}
		res.Underlay.EVPN.VtepIP = vtepIP.String()
	}

	for _, vni := range apiConfig.L3VNIs {
		v := hostnetwork.L3VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:           vni.Spec.VRF,
				TargetNS:      targetNS,
				VTEPIP:        res.Underlay.EVPN.VtepIP,
				VTEPInterface: underlay.Spec.EVPN.VTEPInterface,
				VNI:           int(vni.Spec.VNI),
				VXLanPort:     int(vni.Spec.VXLanPort),
			},
		}
		if vni.Spec.HostSession == nil {
			res.L3VNIs = append(res.L3VNIs, v)
			continue
		}

		vethIPs, err := ipam.VethIPsFromPool(vni.Spec.HostSession.LocalCIDR.IPv4, vni.Spec.HostSession.LocalCIDR.IPv6, nodeIndex)
		if err != nil {
			return res, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d", vni.Spec.HostSession.LocalCIDR, nodeIndex)
		}

		v.HostVeth = &hostnetwork.Veth{
			HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
			NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
			HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
			NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
		}

		res.L3VNIs = append(res.L3VNIs, v)
	}

	res.L2VNIs = []hostnetwork.L2VNIParams{}
	for _, l2vni := range apiConfig.L2VNIs {
		vni := hostnetwork.L2VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:           l2vni.VRFName(),
				TargetNS:      targetNS,
				VTEPIP:        res.Underlay.EVPN.VtepIP,
				VTEPInterface: underlay.Spec.EVPN.VTEPInterface,
				VNI:           int(l2vni.Spec.VNI),
				VXLanPort:     int(l2vni.Spec.VXLanPort),
			},
		}
		if len(l2vni.Spec.L2GatewayIPs) > 0 {
			vni.L2GatewayIPs = make([]string, len(l2vni.Spec.L2GatewayIPs))
			copy(vni.L2GatewayIPs, l2vni.Spec.L2GatewayIPs)
		}
		if l2vni.Spec.HostMaster != nil {
			var name string
			var autoCreate bool

			switch l2vni.Spec.HostMaster.Type {
			case v1alpha1.LinuxBridge:
				if l2vni.Spec.HostMaster.LinuxBridge != nil {
					name = l2vni.Spec.HostMaster.LinuxBridge.Name
					autoCreate = l2vni.Spec.HostMaster.LinuxBridge.AutoCreate
				}
			case v1alpha1.OVSBridge:
				if l2vni.Spec.HostMaster.OVSBridge != nil {
					name = l2vni.Spec.HostMaster.OVSBridge.Name
					autoCreate = l2vni.Spec.HostMaster.OVSBridge.AutoCreate
				}
			default:
				return HostConfigData{}, fmt.Errorf(
					"unknown host master type %q for L2VNI %s",
					l2vni.Spec.HostMaster.Type,
					client.ObjectKeyFromObject(&l2vni),
				)
			}

			vni.HostMaster = &hostnetwork.HostMaster{
				Name:       name,
				Type:       l2vni.Spec.HostMaster.Type,
				AutoCreate: autoCreate,
			}
		}

		res.L2VNIs = append(res.L2VNIs, vni)
	}

	return res, nil
}

// ipNetToString returns the string representation of the IPNet, or empty string if IP is nil
func ipNetToString(ipNet net.IPNet) string {
	if ipNet.IP == nil {
		return ""
	}
	return ipNet.String()
}
