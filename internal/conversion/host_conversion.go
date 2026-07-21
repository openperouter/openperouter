// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/cni"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func APItoHostConfig(nodeIndex int, targetNS string, apiConfig APIConfigData) (HostConfigData, error) {
	err := validateAPIConfigData(apiConfig)
	e := NoUnderlaysError("")
	if err != nil && errors.As(err, &e) {
		return HostConfigData{
			L3VNIs: []hostnetwork.L3VNIParams{},
			L2VNIs: []hostnetwork.L2VNIParams{},
		}, nil
	}
	if err != nil {
		return HostConfigData{}, err
	}

	underlay := apiConfig.Underlays[0]

	if err := validateTunnelEndpointForHostConfig(underlay.Spec.TunnelEndpoint, apiConfig); err != nil {
		return HostConfigData{}, err
	}

	if len(underlay.Spec.Interfaces) == 0 {
		return HostConfigData{}, errors.New("underlay interface must be specified")
	}

	underlayInterfaces, err := underlayInterfacesToHost(underlay.Spec.Interfaces)
	if err != nil {
		return HostConfigData{}, err
	}

	l3Passthrough, err := passthroughConfigToHost(apiConfig.L3Passthrough, targetNS, nodeIndex)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate passthrough configuration to host, err: %w", err)
	}

	// Thanks to validateTunnelEndpointForHostConfig, we know that if the tunnel endpoint is nil, L3VNIs, L2VNIs and
	// L3VPNs are empty, too, and we must return early here.
	if underlay.Spec.TunnelEndpoint == nil {
		return HostConfigData{
			Underlay: hostnetwork.UnderlayParams{
				TargetNS:           targetNS,
				UnderlayInterfaces: underlayInterfaces,
			},
			L3VNIs:        []hostnetwork.L3VNIParams{},
			L2VNIs:        []hostnetwork.L2VNIParams{},
			L3Passthrough: l3Passthrough,
		}, nil
	}

	underlayConfigTunnelEndpoint, err := tunnelEndpointToHost(underlay.Spec.TunnelEndpoint, nodeIndex)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate tunnel endpoint configuration to host, err: %w", err)
	}

	if err := validateOverlayPrerequisitesForHost(apiConfig, underlayConfigTunnelEndpoint); err != nil {
		return HostConfigData{}, err
	}

	l3VNIs, err := l3vnisToHost(
		apiConfig.L3VNIs,
		underlayConfigTunnelEndpoint,
		targetNS,
		nodeIndex)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate L3VNIs to host, err: %w", err)
	}

	vrfMap := createVRFMap(apiConfig.L3VNIs, apiConfig.L3VPNs)
	l2VNIs, err := l2vnisToHost(
		apiConfig.L2VNIs,
		underlayConfigTunnelEndpoint,
		targetNS,
		vrfMap)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate L2VNIs to host, err: %w", err)
	}

	l3VPNs, err := l3vpnsToHost(
		apiConfig.L3VPNs,
		underlay.Spec.SRV6,
		targetNS,
		nodeIndex)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate L3VPNs to host, err: %w", err)
	}

	return HostConfigData{
		Underlay: hostnetwork.UnderlayParams{
			TargetNS:           targetNS,
			UnderlayInterfaces: underlayInterfaces,
			TunnelEndpoint:     &underlayConfigTunnelEndpoint,
		},
		L3VNIs:        l3VNIs,
		L2VNIs:        l2VNIs,
		L3VPNs:        l3VPNs,
		L3Passthrough: l3Passthrough,
	}, nil
}

// validateTunnelEndpointForHostConfig makes sure that whenever L3VNIs, L2VNIs or L3VPNs are set, the tunnelEndpoint
// must be configured, too.
func validateTunnelEndpointForHostConfig(tunnelEndpoint *v1alpha1.TunnelEndpointConfig, apiConfig APIConfigData) error {
	if tunnelEndpoint != nil {
		return nil
	}

	var errs []error
	if len(apiConfig.L3VNIs) > 0 {
		errs = append(errs, fmt.Errorf("underlay tunnel endpoint configuration is required when L3VNIs are defined"))
	}
	if len(apiConfig.L2VNIs) > 0 {
		errs = append(errs, fmt.Errorf("underlay tunnel endpoint configuration is required when L2VNIs are defined"))
	}
	if len(apiConfig.L3VPNs) > 0 {
		errs = append(errs, fmt.Errorf("underlay tunnel endpoint configuration is required when L3VPNs are defined"))
	}
	return errors.Join(errs...)
}

func passthroughConfigToHost(l3Passthrough []v1alpha1.L3Passthrough, targetNS string,
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
		LinkIPs: hostnetwork.LinkIPs{
			HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
			NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
			HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
			NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
		},
	}, nil
}

func validateOverlayPrerequisitesForHost(config APIConfigData, tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams) error {
	underlay := config.Underlays[0]

	var errs []error
	if len(config.L3VNIs) > 0 && tunnelEndpoint.IPv4CIDR == "" && tunnelEndpoint.IPv6CIDR == "" {
		errs = append(errs, errors.New("tunnel endpoint IPv4 or IPv6 configuration is required when L3VNIs are defined"))
	}
	if len(config.L2VNIs) > 0 && tunnelEndpoint.IPv4CIDR == "" && tunnelEndpoint.IPv6CIDR == "" {
		errs = append(errs, errors.New("tunnel endpoint IPv4 or IPv6 configuration is required when L2VNIs are defined"))
	}
	if len(config.L3VPNs) > 0 && tunnelEndpoint.IPv6CIDR == "" {
		errs = append(errs, errors.New("tunnel endpoint IPv6 configuration is required when L3VPNs are defined"))
	}
	if len(config.L3VPNs) > 0 && underlay.Spec.SRV6 == nil {
		errs = append(errs, errors.New("SRV6 configuration is required when L3VPNs are defined"))
	}
	if underlay.Spec.SRV6 != nil && underlay.Spec.ISIS == nil {
		errs = append(errs, errors.New("ISIS configuration is required when SRv6 is defined"))
	}

	return errors.Join(errs...)
}

func tunnelEndpointToHost(tunnelEndpointConfig *v1alpha1.TunnelEndpointConfig, nodeIndex int) (hostnetwork.UnderlayTunnelEndpointParams, error) {
	tunnelEndpoint := hostnetwork.UnderlayTunnelEndpointParams{}
	for _, cidr := range tunnelEndpointConfig.CIDRs {
		af := ipfamily.ForCIDRString(cidr)
		if af == ipfamily.Unknown {
			return hostnetwork.UnderlayTunnelEndpointParams{},
				fmt.Errorf("failed to determine address family for CIDR %q", cidr)
		}

		ip, err := ipam.TunnelEndpointIP(cidr, nodeIndex)
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
				tunnelEndpointConfig.CIDRs)
	}
	return tunnelEndpoint, nil
}

func l3vnisToHost(l3vnis []v1alpha1.L3VNI, tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams,
	targetNS string, nodeIndex int) ([]hostnetwork.L3VNIParams, error) {
	hostL3VNIs := []hostnetwork.L3VNIParams{}
	for _, l3vni := range l3vnis {
		hostL3VNI, err := l3vniToHost(l3vni, tunnelEndpoint, targetNS, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to translate L3VNI %s, err: %w", l3vni.Name, err)
		}
		hostL3VNIs = append(hostL3VNIs, hostL3VNI)
	}
	return hostL3VNIs, nil
}

func l3vniToHost(l3vni v1alpha1.L3VNI, tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams, targetNS string, nodeIndex int) (hostnetwork.L3VNIParams, error) {
	vtepIP, err := resolveVTEPIP(l3vni.Spec.UnderlayAddressFamily, tunnelEndpoint)
	if err != nil {
		return hostnetwork.L3VNIParams{}, fmt.Errorf("L2VNI %s: %w", l3vni.Name, err)
	}

	hostL3VNI := hostnetwork.L3VNIParams{
		Name: l3vni.Name,
		VNIParams: hostnetwork.VNIParams{
			VRF:       l3vni.Spec.VRF,
			TargetNS:  targetNS,
			VTEPIP:    vtepIP,
			VNI:       l3vni.Spec.VNI,
			VXLanPort: vxlanPort(l3vni.Spec.VXLanPort),
		},
	}
	if l3vni.Spec.HostSession == nil {
		return hostL3VNI, nil
	}

	vethIPs, err := ipam.VethIPsFromPool(
		l3vni.Spec.HostSession.LocalCIDR.IPv4,
		l3vni.Spec.HostSession.LocalCIDR.IPv6,
		nodeIndex)
	if err != nil {
		return hostnetwork.L3VNIParams{}, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d, err: %w",
			l3vni.Spec.HostSession.LocalCIDR, nodeIndex, err)
	}

	hostL3VNI.LinkIPs = &hostnetwork.LinkIPs{
		HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
		NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
		HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
		NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
	}

	return hostL3VNI, nil
}

func l2vnisToHost(
	l2vnis []v1alpha1.L2VNI,
	tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams,
	targetNS string,
	vrfMap map[string]string,
) ([]hostnetwork.L2VNIParams, error) {
	hostL2VNIs := []hostnetwork.L2VNIParams{}
	for _, l2vni := range l2vnis {
		vni, err := l2vniToHost(l2vni, tunnelEndpoint, targetNS, vrfMap)
		if err != nil {
			return nil, fmt.Errorf("failed to translate L2VNI %s, err: %w", l2vni.Name, err)
		}
		hostL2VNIs = append(hostL2VNIs, vni)
	}
	return hostL2VNIs, nil
}

func l2vniToHost(
	l2vni v1alpha1.L2VNI,
	tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams,
	targetNS string,
	vrfMap map[string]string,
) (hostnetwork.L2VNIParams, error) {
	vtepIP, err := resolveVTEPIP(l2vni.Spec.UnderlayAddressFamily, tunnelEndpoint)
	if err != nil {
		return hostnetwork.L2VNIParams{}, fmt.Errorf("L2VNI %s: %w", l2vni.Name, err)
	}

	hostL2VNI := hostnetwork.L2VNIParams{
		Name: l2vni.Name,
		VNIParams: hostnetwork.VNIParams{
			TargetNS:  targetNS,
			VTEPIP:    vtepIP,
			VNI:       l2vni.Spec.VNI,
			VXLanPort: vxlanPort(l2vni.Spec.VXLanPort),
		},
	}
	if hasRoutingDomain(l2vni) {
		hostL2VNI.VRF = resolveVRFForL2VNI(l2vni, vrfMap)
	}
	if len(l2vni.Spec.GatewayIPs) > 0 {
		hostL2VNI.L2GatewayIPs = make([]string, len(l2vni.Spec.GatewayIPs))
		copy(hostL2VNI.L2GatewayIPs, l2vni.Spec.GatewayIPs)
	}
	if l2vni.Spec.HostMaster != nil {
		hm, err := convertHostMaster(&l2vni)
		if err != nil {
			return hostnetwork.L2VNIParams{}, err
		}
		hostL2VNI.HostMaster = hm
	}
	return hostL2VNI, nil
}

func l3vpnsToHost(l3vpns []v1alpha1.L3VPN, srv6Config *v1alpha1.SRV6Config,
	targetNS string, nodeIndex int) ([]hostnetwork.L3VPNParams, error) {
	if srv6Config == nil {
		return []hostnetwork.L3VPNParams{}, nil
	}
	hostL3VPNs := []hostnetwork.L3VPNParams{}
	for _, l3vpn := range l3vpns {
		hostL3VPN, err := l3vpnToHost(l3vpn, targetNS, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to translate L3VPN %s, err: %w", l3vpn.Name, err)
		}
		hostL3VPNs = append(hostL3VPNs, hostL3VPN)
	}
	return hostL3VPNs, nil
}

// l3vpnToHost converts a single API L3VPN custom resource into a hostnetwork.L3VPNParams.
// On the host side, we need to create unique interfaces. As RDAssignedNumber is a unique integer, we use that
// as the numeric interface identifier, analogous to VNI for L2VNI / L3VNI.
func l3vpnToHost(l3vpn v1alpha1.L3VPN, targetNS string, nodeIndex int) (hostnetwork.L3VPNParams, error) {
	hostL3VPN := hostnetwork.L3VPNParams{
		Name:             l3vpn.Name,
		VRF:              l3vpn.Spec.VRF,
		TargetNS:         targetNS,
		RDAssignedNumber: l3vpn.Spec.RDAssignedNumber,
	}
	if l3vpn.Spec.HostSession == nil {
		return hostL3VPN, nil
	}

	vethIPs, err := ipam.VethIPsFromPool(
		l3vpn.Spec.HostSession.LocalCIDR.IPv4,
		l3vpn.Spec.HostSession.LocalCIDR.IPv6,
		nodeIndex)
	if err != nil {
		return hostnetwork.L3VPNParams{}, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d, err: %w",
			l3vpn.Spec.HostSession.LocalCIDR, nodeIndex, err)
	}

	hostL3VPN.LinkIPs = &hostnetwork.LinkIPs{
		HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
		NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
		HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
		NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
	}

	return hostL3VPN, nil
}

func vxlanPort(p *int32) *int32 {
	if p == nil {
		return new(int32(4789))
	}
	return p
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
	tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams,
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

// underlayInterfacesToHost translates the underlay interfaces union into the
// unified host representation, unmarshalling the
// opaque runtimeConfig of the CNI-provisioned interfaces into the capability
// arguments handed to the plugin. It rejects duplicate or invalid interface
// names, invalid CNI configurations and mixes of NetworkDevice and CNIDevice
// interfaces.
func underlayInterfacesToHost(interfaces []v1alpha1.UnderlayInterface) ([]hostnetwork.UnderlayInterface, error) {
	res := make([]hostnetwork.UnderlayInterface, 0, len(interfaces))
	seenNames := sets.Set[string]{}
	kinds := sets.Set[hostnetwork.UnderlayInterfaceKind]{}
	for _, iface := range interfaces {
		hostIface, err := underlayInterfaceToHost(iface)
		if err != nil {
			return nil, err
		}
		if seenNames.Has(hostIface.InterfaceName) {
			return nil, fmt.Errorf("duplicate underlay interface name %s", hostIface.InterfaceName)
		}
		seenNames.Insert(hostIface.InterfaceName)
		if err := isValidInterfaceName(hostIface.InterfaceName); err != nil {
			return nil, fmt.Errorf("invalid interface name %s: %w", hostIface.InterfaceName, err)
		}
		if hostIface.Kind == hostnetwork.UnderlayInterfaceCNIDev {
			if err := cni.ValidateConfig(hostIface.CNI.Config); err != nil {
				return nil, fmt.Errorf("invalid cni config for interface %s: %w", hostIface.InterfaceName, err)
			}
		}
		if kinds.Insert(hostIface.Kind).Len() > 1 {
			return nil, errors.New("all interfaces must be of the same type")
		}
		res = append(res, hostIface)
	}
	return res, nil
}

// underlayInterfaceToHost translates a single underlay interface union entry
// into its name and host representation.
func underlayInterfaceToHost(iface v1alpha1.UnderlayInterface) (hostnetwork.UnderlayInterface, error) {
	switch iface.Type {
	case v1alpha1.UnderlayInterfaceTypeNetworkDevice:
		return networkDeviceInterfaceToHost(iface)
	case v1alpha1.UnderlayInterfaceTypeCNIDevice:
		return cniDeviceInterfaceToHost(iface)
	default:
		return hostnetwork.UnderlayInterface{}, fmt.Errorf("unsupported underlay interface type %q", iface.Type)
	}
}

func networkDeviceInterfaceToHost(iface v1alpha1.UnderlayInterface) (hostnetwork.UnderlayInterface, error) {
	if iface.NetworkDevice == nil {
		return hostnetwork.UnderlayInterface{},
			fmt.Errorf("networkDevice configuration is missing for interface type NetworkDevice")
	}
	if iface.NetworkDevice.InterfaceName == "" {
		return hostnetwork.UnderlayInterface{}, fmt.Errorf("interfaceName is empty for networkDevice")
	}
	return hostnetwork.UnderlayInterface{
		InterfaceName: iface.NetworkDevice.InterfaceName,
		Kind:          hostnetwork.UnderlayInterfaceNetDev,
	}, nil
}

func cniDeviceInterfaceToHost(iface v1alpha1.UnderlayInterface) (hostnetwork.UnderlayInterface, error) {
	if iface.CNIDevice == nil {
		return hostnetwork.UnderlayInterface{},
			fmt.Errorf("cniDevice configuration is missing for interface type CNI")
	}
	if iface.CNIDevice.RawConfig == nil {
		return hostnetwork.UnderlayInterface{},
			fmt.Errorf("rawConfig is missing for cniDevice %q", ptr.Deref(iface.CNIDevice.InterfaceName, ""))
	}

	ifName := ptr.Deref(iface.CNIDevice.InterfaceName, cni.DefaultInterfaceName)
	if ifName == "" {
		ifName = cni.DefaultInterfaceName
	}

	var capabilityArgs map[string]any
	if iface.CNIDevice.RuntimeConfig != nil {
		if err := json.Unmarshal(iface.CNIDevice.RuntimeConfig.Raw, &capabilityArgs); err != nil {
			return hostnetwork.UnderlayInterface{},
				fmt.Errorf("invalid runtimeConfig for cni interface %q: %w", ifName, err)
		}
	}

	return hostnetwork.UnderlayInterface{
		InterfaceName: ifName,
		Kind:          hostnetwork.UnderlayInterfaceCNIDev,
		CNI: &hostnetwork.CNIDeviceParams{
			Config:         iface.CNIDevice.RawConfig.Raw,
			CapabilityArgs: capabilityArgs,
		},
	}, nil
}
