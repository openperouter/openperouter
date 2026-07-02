// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"k8s.io/utils/ptr"

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

	if len(underlay.Spec.Interfaces) == 0 {
		return res, fmt.Errorf("underlay interface must be specified")
	}

	underlayInterfaces, err := underlayNetworkDeviceInterfaceNames(underlay.Spec.Interfaces)
	if err != nil {
		return res, err
	}
	res.Underlay = hostnetwork.UnderlayParams{
		TargetNS:           targetNS,
		UnderlayInterfaces: underlayInterfaces,
	}

	cniInterfaces, err := underlayCNIInterfaces(underlay.Spec.Interfaces)
	if err != nil {
		return res, err
	}
	if len(cniInterfaces) > 0 {
		res.Underlay.CNI = &hostnetwork.UnderlayCNIParams{
			Interfaces: cniInterfaces,
			Runtime:    apiConfig.CNIRuntime,
		}
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
		vtepIP, err := resolveVTEPIP(vni.Spec.UnderlayAddressFamily, res.Underlay.TunnelEndpoint)
		if err != nil {
			return HostConfigData{}, fmt.Errorf("L3VNI %s: %w", vni.Name, err)
		}
		v := hostnetwork.L3VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:       vni.Spec.VRF,
				TargetNS:  targetNS,
				VTEPIP:    vtepIP,
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
		vni, err := convertL2VNI(l2vni, targetNS, res.Underlay.TunnelEndpoint)
		if err != nil {
			return HostConfigData{}, err
		}
		res.L2VNIs = append(res.L2VNIs, vni)
	}

	return res, nil
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
	if tunnelEndpoint.IPv4CIDR == "" && tunnelEndpoint.IPv6CIDR == "" {
		return hostnetwork.UnderlayTunnelEndpointParams{},
			fmt.Errorf("no VTEP IP available after conversion from tunnel endpoint CIDRs: %v",
				underlay.Spec.TunnelEndpoint.CIDRs)
	}
	return tunnelEndpoint, nil
}

func convertL2VNI(l2vni v1alpha1.L2VNI, targetNS string, tunnelEndpoint *hostnetwork.UnderlayTunnelEndpointParams) (hostnetwork.L2VNIParams, error) {
	vtepIP, err := resolveVTEPIP(l2vni.Spec.UnderlayAddressFamily, tunnelEndpoint)
	if err != nil {
		return hostnetwork.L2VNIParams{}, fmt.Errorf("L2VNI %s: %w", l2vni.Name, err)
	}
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

func resolveVTEPIP(
	underlayAddressFamily *string,
	tunnelEndpoint *hostnetwork.UnderlayTunnelEndpointParams,
) (string, error) {
	af := ""
	if underlayAddressFamily != nil {
		af = *underlayAddressFamily
	}

	switch af {
	case "":
		if tunnelEndpoint.IPv4CIDR != "" {
			return tunnelEndpoint.IPv4CIDR, nil
		}
		if tunnelEndpoint.IPv6CIDR != "" {
			return tunnelEndpoint.IPv6CIDR, nil
		}
		return "", fmt.Errorf("no VTEP IP available")
	case "ipv4":
		if tunnelEndpoint.IPv4CIDR != "" {
			return tunnelEndpoint.IPv4CIDR, nil
		}
		return "", fmt.Errorf("ipv4 address family requested but no IPv4 VTEP IP available")
	case "ipv6":
		if tunnelEndpoint.IPv6CIDR != "" {
			return tunnelEndpoint.IPv6CIDR, nil
		}
		return "", fmt.Errorf("ipv6 address family requested but no IPv6 VTEP IP available")
	default:
		return "", fmt.Errorf("unsupported address family %q", af)
	}
}

func ipNetToString(ipNet net.IPNet) string {
	if ipNet.IP == nil {
		return ""
	}
	return ipNet.String()
}

// underlayNetworkDeviceInterfaceNames extracts the host interface names from the underlay
// interfaces list. Entries of other modes (e.g. CNI) are skipped.
func underlayNetworkDeviceInterfaceNames(interfaces []v1alpha1.UnderlayInterface) ([]string, error) {
	names := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Type != v1alpha1.UnderlayInterfaceTypeNetworkDevice {
			continue
		}
		if iface.NetworkDevice == nil {
			return nil, fmt.Errorf("networkDevice configuration is missing for interface type NetworkDevice")
		}

		if iface.NetworkDevice.InterfaceName == "" {
			return nil, fmt.Errorf("interfaceName is empty for networkDevice")
		}

		names = append(names, iface.NetworkDevice.InterfaceName)
	}
	return names, nil
}

// defaultCNIInterfaceName is the interface name used for a CNI-provisioned
// underlay interface when the spec omits it. It matches the API-level default
// applied by the CRD schema.
const defaultCNIInterfaceName = "net1"

// underlayCNIInterfaces extracts the CNI-provisioned interfaces from the
// underlay interfaces list, unmarshalling the opaque runtimeConfig into the
// capability arguments handed to the plugin.
func underlayCNIInterfaces(interfaces []v1alpha1.UnderlayInterface) ([]hostnetwork.CNIInterfaceParams, error) {
	res := make([]hostnetwork.CNIInterfaceParams, 0, len(interfaces))
	for _, iface := range interfaces {
		if iface.Type != v1alpha1.UnderlayInterfaceTypeCNI {
			continue
		}
		if iface.CNIDevice == nil {
			return nil, fmt.Errorf("cniDevice configuration is missing for interface type CNI")
		}
		if iface.CNIDevice.RawConfig == nil {
			return nil, fmt.Errorf("rawConfig is missing for cniDevice %q", ptr.Deref(iface.CNIDevice.InterfaceName, ""))
		}

		ifName := ptr.Deref(iface.CNIDevice.InterfaceName, defaultCNIInterfaceName)
		if ifName == "" {
			ifName = defaultCNIInterfaceName
		}

		var capabilityArgs map[string]interface{}
		if iface.CNIDevice.RuntimeConfig != nil {
			if err := json.Unmarshal(iface.CNIDevice.RuntimeConfig.Raw, &capabilityArgs); err != nil {
				return nil, fmt.Errorf("invalid runtimeConfig for cni interface %q: %w", ifName, err)
			}
		}

		res = append(res, hostnetwork.CNIInterfaceParams{
			Config:         iface.CNIDevice.RawConfig.Raw,
			IfName:         ifName,
			CapabilityArgs: capabilityArgs,
		})
	}
	return res, nil
}
