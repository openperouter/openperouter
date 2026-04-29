// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
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

	// Common validation between the FRR and Host config conversion layer.
	if err := validateAPIConfigData(apiConfig); err != nil {
		return HostConfigData{}, err
	}

	underlay := apiConfig.Underlays[0]

	underlayInterface, err := getUnderlayInterfaceForHost(underlay, underlayFromMultus)
	if err != nil {
		return HostConfigData{}, err
	}

	l3Passthrough, err := convertPassthroughConfigForHost(apiConfig.L3Passthrough, targetNS, nodeIndex)
	if err != nil {
		return HostConfigData{}, err
	}

	underlayConfigEVPN, err := convertUnderlayEVPNForHost(underlay, nodeIndex)
	if err != nil {
		return HostConfigData{}, err
	}

	l3VNIs, err := convertL3VNIsForHost(
		underlay,
		apiConfig.L3VNIs,
		underlayConfigEVPN.VtepIP,
		targetNS,
		nodeIndex)
	if err != nil {
		return HostConfigData{}, err
	}

	l2VNIs, err := convertL2VNIsForHost(
		underlay,
		apiConfig.L2VNIs,
		underlayConfigEVPN.VtepIP,
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

// getUnderlayInterfaceForHost returns the underlay interface name from the Underlay spec.
// It is a helper for APItoHostConfig to keep cognitive complexity light.
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

// convertL2VNIsForHost converts the L2VNI API resources into a slice of L2VNIParams for host network
// configuration (EVPN). It is a helper for APItoHostConfig to keep cognitive complexity light.
func convertL2VNIsForHost(underlay v1alpha1.Underlay, l2VNIs []v1alpha1.L2VNI,
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
				VNI:           int(l2vni.Spec.VNI),
				VXLanPort:     int(l2vni.Spec.VXLanPort),
			},
			L2GatewayIPs: l2vni.Spec.L2GatewayIPs,
		}
		if l2vni.Spec.HostMaster != nil {
			hm, err := convertHostMaster(&l2vni)
			if err != nil {
				return nil, err
			}
			vni.HostMaster = hm
		}
		hostVNIs = append(hostVNIs, vni)
	}
	return hostVNIs, nil
}

// convertL3VNIsForHost converts the L3VNI API resources into a slice of L3VNIParams for host network
// configuration (EVPN). It is a helper for APItoHostConfig to keep cognitive complexity light.
func convertL3VNIsForHost(underlay v1alpha1.Underlay, l3VNIs []v1alpha1.L3VNI,
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
				VNI:           int(vni.Spec.VNI),
				VXLanPort:     int(vni.Spec.VXLanPort),
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

// convertUnderlayEVPNForHost converts the EVPN underlay VTEP CIDR into UnderlayEVPNParams.
// It is a helper for APItoHostConfig to keep cognitive complexity light.
func convertUnderlayEVPNForHost(underlay v1alpha1.Underlay, nodeIndex int) (hostnetwork.UnderlayEVPNParams, error) {
	if underlay.Spec.EVPN == nil {
		return hostnetwork.UnderlayEVPNParams{}, nil
	}
	if underlay.Spec.EVPN.VTEPCIDR == "" {
		return hostnetwork.UnderlayEVPNParams{}, nil
	}

	vtepIP, err := ipam.VTEPIp(underlay.Spec.EVPN.VTEPCIDR, nodeIndex)
	if err != nil {
		return hostnetwork.UnderlayEVPNParams{}, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w",
			underlay.Spec.EVPN.VTEPCIDR, nodeIndex, err)
	}
	return hostnetwork.UnderlayEVPNParams{
		VtepIP: vtepIP.String(),
	}, nil
}

// convertPassthroughConfigForHost converts the L3Passthrough API resources into PassthroughParams for host network
// configuration. It is a helper for APItoHostConfig to keep cognitive complexity light.
func convertPassthroughConfigForHost(l3Passthrough []v1alpha1.L3Passthrough, targetNS string,
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

func convertHostMaster(l2vni *v1alpha1.L2VNI) (*hostnetwork.HostMaster, error) {
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
