// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// APItoHostConfig converts API custom resources into configuration that is applied to the underlying OS.
func APItoHostConfig(nodeIndex int, targetNS string, underlayFromMultus bool,
	apiConfig APIConfigData) (HostConfigData, error) {
	// Contrary to the FRR conversion, the Host conversion always accepts an empty underlay.
	if len(apiConfig.Underlays) == 0 {
		return HostConfigData{
			L3VNIs: []hostnetwork.L3VNIParams{},
			L2VNIs: []hostnetwork.L2VNIParams{},
		}, nil
	}

	if err := validateAPIConfigData(apiConfig); err != nil {
		return HostConfigData{}, err
	}

	underlay := apiConfig.Underlays[0]

	underlayInterface, err := getUnderlayInterfaceForHost(underlay, underlayFromMultus)
	if err != nil {
		return HostConfigData{}, err
	}

	l3Passthrough, err := apiToHostPassthrough(apiConfig.L3Passthrough, targetNS, nodeIndex)
	if err != nil {
		return HostConfigData{}, err
	}

	underlayConfigEVPN, err := apiToHostEVPN(underlay, nodeIndex)
	if err != nil {
		return HostConfigData{}, err
	}
	vtepIP := ""
	if underlayConfigEVPN != nil {
		vtepIP = underlayConfigEVPN.VtepIP
	}

	l3VNIs, err := apiToHostL3VNIs(
		underlay,
		apiConfig.L3VNIs,
		vtepIP,
		targetNS,
		nodeIndex)
	if err != nil {
		return HostConfigData{}, err
	}

	l2VNIs, err := apiToHostL2VNIs(
		underlay,
		apiConfig.L2VNIs,
		vtepIP,
		targetNS)
	if err != nil {
		return HostConfigData{}, err
	}

	return HostConfigData{
		Underlay: hostnetwork.UnderlayParams{
			TargetNS:          targetNS,
			UnderlayInterface: underlayInterface,
			EVPN:              underlayConfigEVPN,
		},
		L3VNIs:        l3VNIs,
		L2VNIs:        l2VNIs,
		L3Passthrough: l3Passthrough,
	}, nil
}

func getUnderlayInterfaceForHost(underlay v1alpha1.Underlay, underlayFromMultus bool) (string, error) {
	if len(underlay.Spec.Nics) == 0 && !underlayFromMultus {
		return "", errors.New("underlay interface must be specified when Multus is not enabled")
	}

	underlayInterface := ""
	if len(underlay.Spec.Nics) > 0 {
		underlayInterface = underlay.Spec.Nics[0]
	}
	return underlayInterface, nil
}

func apiToHostPassthrough(l3Passthrough []v1alpha1.L3Passthrough, targetNS string,
	nodeIndex int) (*hostnetwork.PassthroughParams, error) {
	if len(l3Passthrough) != 1 {
		return nil, nil
	}
	vethIPs, err := ipam.VethIPsFromPool(
		l3Passthrough[0].Spec.HostSession.LocalCIDR.IPv4,
		l3Passthrough[0].Spec.HostSession.LocalCIDR.IPv6,
		nodeIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d, err: %w",
			l3Passthrough[0].Spec.HostSession.LocalCIDR, nodeIndex, err)
	}

	return &hostnetwork.PassthroughParams{
		TargetNS: targetNS,
		HostVeth: hostnetwork.Veth{
			HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
			NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
			HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
			NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
		},
	}, nil
}

func apiToHostEVPN(underlay v1alpha1.Underlay, nodeIndex int) (*hostnetwork.UnderlayEVPNParams, error) {
	if underlay.Spec.EVPN == nil {
		return nil, nil
	}
	vtepCIDR := ptr.Deref(underlay.Spec.EVPN.VTEPCIDR, "")
	if vtepCIDR == "" {
		return &hostnetwork.UnderlayEVPNParams{}, nil
	}

	vtepIP, err := ipam.VTEPIp(vtepCIDR, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w",
			vtepCIDR, nodeIndex, err)
	}
	return &hostnetwork.UnderlayEVPNParams{
		VtepIP: vtepIP.String(),
	}, nil
}

func apiToHostL3VNIs(underlay v1alpha1.Underlay, l3VNIs []v1alpha1.L3VNI,
	vtepIP, targetNS string, nodeIndex int) ([]hostnetwork.L3VNIParams, error) {
	if underlay.Spec.EVPN == nil {
		return []hostnetwork.L3VNIParams{}, nil
	}
	hostVNIs := []hostnetwork.L3VNIParams{}
	for _, vni := range l3VNIs {
		v := hostnetwork.L3VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:           vni.Spec.VRF,
				TargetNS:      targetNS,
				VTEPIP:        vtepIP,
				VTEPInterface: underlay.Spec.EVPN.VTEPInterface,
				VNI:           vni.Spec.VNI,
				VXLanPort:     vni.Spec.VXLanPort,
			},
		}
		if vni.Spec.HostSession == nil {
			hostVNIs = append(hostVNIs, v)
			continue
		}

		vethIPs, err := ipam.VethIPsFromPool(
			vni.Spec.HostSession.LocalCIDR.IPv4,
			vni.Spec.HostSession.LocalCIDR.IPv6,
			nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d, err: %w",
				vni.Spec.HostSession.LocalCIDR, nodeIndex, err)
		}

		v.HostVeth = &hostnetwork.Veth{
			HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
			NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
			HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
			NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
		}

		hostVNIs = append(hostVNIs, v)
	}
	return hostVNIs, nil
}

func apiToHostL2VNIs(underlay v1alpha1.Underlay, l2VNIs []v1alpha1.L2VNI,
	vtepIP, targetNS string) ([]hostnetwork.L2VNIParams, error) {
	if underlay.Spec.EVPN == nil {
		return []hostnetwork.L2VNIParams{}, nil
	}
	hostVNIs := []hostnetwork.L2VNIParams{}
	for _, l2vni := range l2VNIs {
		vni := hostnetwork.L2VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:           l2vni.VRFName(),
				TargetNS:      targetNS,
				VTEPIP:        vtepIP,
				VTEPInterface: underlay.Spec.EVPN.VTEPInterface,
				VNI:           l2vni.Spec.VNI,
				VXLanPort:     l2vni.Spec.VXLanPort,
			},
			L2GatewayIPs: append([]string(nil), l2vni.Spec.L2GatewayIPs...), // Create deepcopy to avoid side effects.
		}
		if l2vni.Spec.HostMaster != nil {
			hm, err := apiToHostMaster(&l2vni)
			if err != nil {
				return nil, err
			}
			vni.HostMaster = hm
		}
		hostVNIs = append(hostVNIs, vni)
	}
	return hostVNIs, nil
}

func apiToHostMaster(l2vni *v1alpha1.L2VNI) (*hostnetwork.HostMaster, error) {
	switch l2vni.Spec.HostMaster.Type {
	case v1alpha1.LinuxBridge:
		if l2vni.Spec.HostMaster.LinuxBridge != nil {
			return &hostnetwork.HostMaster{
				Name:       l2vni.Spec.HostMaster.LinuxBridge.Name,
				Type:       l2vni.Spec.HostMaster.Type,
				AutoCreate: l2vni.Spec.HostMaster.LinuxBridge.AutoCreate,
			}, nil
		}
	case v1alpha1.OVSBridge:
		if l2vni.Spec.HostMaster.OVSBridge != nil {
			return &hostnetwork.HostMaster{
				Name:       l2vni.Spec.HostMaster.OVSBridge.Name,
				Type:       l2vni.Spec.HostMaster.Type,
				AutoCreate: l2vni.Spec.HostMaster.OVSBridge.AutoCreate,
			}, nil
		}
	default:
		return nil, fmt.Errorf(
			"unknown host master type %q for L2VNI %s",
			l2vni.Spec.HostMaster.Type,
			client.ObjectKeyFromObject(l2vni),
		)
	}

	return nil, fmt.Errorf(
		"host master config is nil for type %q in L2VNI %s",
		l2vni.Spec.HostMaster.Type,
		client.ObjectKeyFromObject(l2vni),
	)
}

// ipNetToString returns the string representation of the IPNet, or empty string if IP is nil
func ipNetToString(ipNet net.IPNet) string {
	if ipNet.IP == nil {
		return ""
	}
	return ipNet.String()
}
