// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func APItoHostConfig(nodeIndex int, targetNS string, apiConfig APIConfigData) (HostConfigData, error) {
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

	res.Underlay = hostnetwork.UnderlayParams{
		TargetNS: targetNS,
	}
	if err := underlayInterfacesToHost(nodeIndex, underlay, &apiConfig, &res.Underlay); err != nil {
		return res, err
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
	if underlay.Spec.TunnelEndpoint == nil && (len(apiConfig.L3VNIs) > 0 || len(apiConfig.L2VNIs) > 0) {
		return res, fmt.Errorf("underlay tunnel endpoint configuration is required when L3 or L2 VNIs are defined")
	}

	if underlay.Spec.TunnelEndpoint == nil {
		return res, nil
	}

	underlayConfigTunnelEndpoint, err := tunnelEndpointToHost(underlay, nodeIndex)
	if err != nil {
		return HostConfigData{}, err
	}
	res.Underlay.TunnelEndpoint = &underlayConfigTunnelEndpoint

	for _, vni := range apiConfig.L3VNIs {
		v := hostnetwork.L3VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:       vni.Spec.VRF,
				TargetNS:  targetNS,
				VTEPIP:    res.Underlay.TunnelEndpoint.IPv4CIDR,
				VNI:       vni.Spec.VNI,
				VXLanPort: vni.Spec.VXLanPort,
			},
			Name: vni.Name,
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
		vni, err := convertL2VNI(l2vni, targetNS, res.Underlay.TunnelEndpoint.IPv4CIDR)
		if err != nil {
			return HostConfigData{}, err
		}
		res.L2VNIs = append(res.L2VNIs, vni)
	}

	return res, nil
}

// underlayCNIIfName is the interface name created inside the router netns when
// the underlay is provisioned from a NetworkAttachmentDefinition.
const underlayCNIIfName = "underlay0"

// underlayInterfacesToHost fills the interface portion of UnderlayParams either
// from physical nics (moved into the namespace) or from a
// NetworkAttachmentDefinition (provisioned via CNI). For the NAD path, when
// cidrs are configured the per-node IPs are computed from them and handed to the
// IPAM plugin via the CNI "ips" capability (the NAD must declare
// `capabilities.ips=true` and an IPAM that honours it, e.g. static); when no
// cidrs are configured the NAD config is passed through unchanged and the NAD's
// own IPAM (e.g. dhcp) assigns the address.
func underlayInterfacesToHost(nodeIndex int, underlay v1alpha1.Underlay, apiConfig *APIConfigData, out *hostnetwork.UnderlayParams) error {
	nadRef := underlay.Spec.NetworkAttachmentDefinition
	if nadRef == nil {
		if len(underlay.Spec.Nics) == 0 {
			return fmt.Errorf("underlay interface must be specified (nics or networkAttachmentDefinition)")
		}
		out.UnderlayInterfaces = make([]string, len(underlay.Spec.Nics))
		copy(out.UnderlayInterfaces, underlay.Spec.Nics)
		return nil
	}

	if apiConfig.UnderlayNAD == nil {
		return fmt.Errorf("underlay references NetworkAttachmentDefinition %q but its config was not resolved", nadRef.Name)
	}
	var addresses []string
	if len(nadRef.CIDRs) > 0 {
		ips, err := ipam.UnderlayIPs(nadRef.CIDRs, nodeIndex)
		if err != nil {
			return fmt.Errorf("failed to assign underlay ips from cidrs %v: %w", nadRef.CIDRs, err)
		}
		addresses = ips
	}
	out.CNI = &hostnetwork.UnderlayCNIParams{
		Config:    []byte(apiConfig.UnderlayNAD.Config),
		IfName:    underlayCNIIfName,
		BinDirs:   apiConfig.CNIBinDirs,
		CacheDir:  apiConfig.CNICacheDir,
		Addresses: addresses,
	}
	return nil
}

func tunnelEndpointToHost(underlay v1alpha1.Underlay, nodeIndex int) (hostnetwork.UnderlayTunnelEndpointParams, error) {
	tunnelEndpoint := hostnetwork.UnderlayTunnelEndpointParams{}
	for _, cidr := range underlay.Spec.TunnelEndpoint.CIDRs {
		af := ipfamily.ForCIDRString(cidr)
		if af == ipfamily.Unknown {
			return hostnetwork.UnderlayTunnelEndpointParams{},
				fmt.Errorf("failed to determine address family for CIDR %q", cidr)
		}

		ip, err := ipam.VTEPIp(cidr, nodeIndex)
		if err != nil {
			return hostnetwork.UnderlayTunnelEndpointParams{},
				fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w", cidr, nodeIndex, err)
		}

		if af == ipfamily.IPv4 {
			tunnelEndpoint.IPv4CIDR = ip.String()
			continue
		}
		tunnelEndpoint.IPv6CIDR = ip.String()
	}
	if tunnelEndpoint.IPv4CIDR == "" {
		return hostnetwork.UnderlayTunnelEndpointParams{},
			fmt.Errorf("no IPv4 CIDR found after conversion from tunnel endpoint CIDRS: %v",
				underlay.Spec.TunnelEndpoint.CIDRs)
	}
	return tunnelEndpoint, nil
}

func convertL2VNI(l2vni v1alpha1.L2VNI, targetNS string, vtepIP string) (hostnetwork.L2VNIParams, error) {
	vni := hostnetwork.L2VNIParams{
		Name: l2vni.Name,
		VNIParams: hostnetwork.VNIParams{
			TargetNS:  targetNS,
			VTEPIP:    vtepIP,
			VNI:       l2vni.Spec.VNI,
			VXLanPort: l2vni.Spec.VXLanPort,
		},
	}
	if hasVRF(l2vni) {
		vni.VRF = *l2vni.Spec.VRF
	}
	if len(l2vni.Spec.L2GatewayIPs) > 0 {
		vni.L2GatewayIPs = make([]string, len(l2vni.Spec.L2GatewayIPs))
		copy(vni.L2GatewayIPs, l2vni.Spec.L2GatewayIPs)
	}
	if l2vni.Spec.HostMaster != nil {
		hm, err := convertHostMaster(&l2vni)
		if err != nil {
			return hostnetwork.L2VNIParams{}, err
		}
		vni.HostMaster = hm
	}
	return vni, nil
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
