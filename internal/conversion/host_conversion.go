// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"net"
	"slices"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func APItoHostConfig(nodeIndex int, targetNS string, apiConfig APIConfigData) (HostConfigData, error) {
	err := validateAPIConfigData(apiConfig)
	e := FRREmptyConfigError("")
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

	underlayInterfaces, err := underlayNicsToHost(underlay)
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

	underlayConfigTunnelEndpoint, err := tunnelEndpointToHost(underlay, nodeIndex)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate tunnel endpoint configuration to host, err: %w", err)
	}

	if err := validateOverlayPrerequisitesForHost(apiConfig, underlayConfigTunnelEndpoint); err != nil {
		return HostConfigData{}, err
	}

	l3VNIs, err := l3vnisToHost(
		apiConfig.L3VNIs,
		underlayConfigTunnelEndpoint.IPv4CIDR,
		targetNS,
		nodeIndex)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate L3VNIs to host, err: %w", err)
	}

	l2VNIs, err := l2vnisToHost(
		apiConfig.L2VNIs,
		underlayConfigTunnelEndpoint.IPv4CIDR,
		targetNS)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate L2VNIs to host, err: %w", err)
	}

	l3VNIsForSRv6, err := l3vpnsToHost(
		underlay,
		apiConfig.L3VPNs,
		underlayConfigTunnelEndpoint.IPv6CIDR,
		targetNS,
		nodeIndex)
	if err != nil {
		return HostConfigData{}, fmt.Errorf("failed to translate L3VPNs to host, err: %w", err)
	}

	// validateAPIConfigData makes sure that we only have either l3VNIs or l3VNIsForSRv6, so we can safely combine them
	// here.
	l3VNIs = append(l3VNIs, l3VNIsForSRv6...)

	return HostConfigData{
		Underlay: hostnetwork.UnderlayParams{
			TargetNS:           targetNS,
			UnderlayInterfaces: underlayInterfaces,
			TunnelEndpoint:     &underlayConfigTunnelEndpoint,
		},
		L3VNIs:        l3VNIs,
		L2VNIs:        l2VNIs,
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

func underlayNicsToHost(underlay v1alpha1.Underlay) ([]string, error) {
	if len(underlay.Spec.Nics) == 0 {
		return nil, errors.New("underlay interface must be specified")
	}
	return slices.Clone(underlay.Spec.Nics), nil
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
		HostVeth: hostnetwork.Veth{
			HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
			NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
			HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
			NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
		},
	}, nil
}

func validateOverlayPrerequisitesForHost(config APIConfigData, tunnelEndpoint hostnetwork.UnderlayTunnelEndpointParams) error {
	underlay := config.Underlays[0]

	if len(config.L3VNIs) > 0 && tunnelEndpoint.IPv4CIDR == "" {
		return errors.New("tunnel endpoint IPv4 configuration is required when L3VNIs are defined")
	}
	if len(config.L2VNIs) > 0 && tunnelEndpoint.IPv4CIDR == "" {
		return errors.New("tunnel endpoint IPv4 configuration is required when L2VNIs are defined")
	}
	if len(config.L3VPNs) > 0 && tunnelEndpoint.IPv6CIDR == "" {
		return errors.New("tunnel endpoint IPv6 configuration is required when L3VPNs are defined")
	}
	if len(config.L3VPNs) > 0 && underlay.Spec.SRV6 == nil {
		return errors.New("SRV6 configuration is required when L3VPNs are defined")
	}
	if underlay.Spec.SRV6 != nil && underlay.Spec.ISIS == nil {
		return errors.New("ISIS configuration is required when SRv6 is defined")
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
	return tunnelEndpoint, nil
}

func l3vnisToHost(l3VNIs []v1alpha1.L3VNI, vtepIP, targetNS string, nodeIndex int) ([]hostnetwork.L3VNIParams, error) {
	hostL3VNIs := []hostnetwork.L3VNIParams{}
	for _, l3vni := range l3VNIs {
		hostL3VNI, err := l3vniToHost(l3vni, vtepIP, targetNS, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to translate L3VNI %s, err: %w", l3vni.Name, err)
		}
		hostL3VNIs = append(hostL3VNIs, hostL3VNI)
	}
	return hostL3VNIs, nil
}

func l3vniToHost(l3vni v1alpha1.L3VNI, vtepIP, targetNS string, nodeIndex int) (hostnetwork.L3VNIParams, error) {
	hostL3VNI := hostnetwork.L3VNIParams{
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

	hostL3VNI.HostVeth = &hostnetwork.Veth{
		HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
		NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
		HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
		NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
	}

	return hostL3VNI, nil
}

func l2vnisToHost(l2VNIs []v1alpha1.L2VNI, vtepIP, targetNS string) ([]hostnetwork.L2VNIParams, error) {
	hostL2VNIs := []hostnetwork.L2VNIParams{}
	for _, l2vni := range l2VNIs {
		vni, err := l2vniToHost(l2vni, targetNS, vtepIP)
		if err != nil {
			return nil, fmt.Errorf("failed to translate L2VNI %s, err: %w", l2vni.Name, err)
		}
		hostL2VNIs = append(hostL2VNIs, vni)
	}
	return hostL2VNIs, nil
}

func l2vniToHost(l2vni v1alpha1.L2VNI, targetNS string, vtepIP string) (hostnetwork.L2VNIParams, error) {
	hostL2VNI := hostnetwork.L2VNIParams{
		VNIParams: hostnetwork.VNIParams{
			TargetNS:  targetNS,
			VTEPIP:    vtepIP,
			VNI:       l2vni.Spec.VNI,
			VXLanPort: vxlanPort(l2vni.Spec.VXLanPort),
		},
	}
	if hasVRF(l2vni) {
		hostL2VNI.VRF = *l2vni.Spec.VRF
	}
	if len(l2vni.Spec.L2GatewayIPs) > 0 {
		hostL2VNI.L2GatewayIPs = make([]string, len(l2vni.Spec.L2GatewayIPs))
		copy(hostL2VNI.L2GatewayIPs, l2vni.Spec.L2GatewayIPs)
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

func l3vpnsToHost(underlay v1alpha1.Underlay, l3VPNs []v1alpha1.L3VPN, vtepIP, targetNS string,
	nodeIndex int) ([]hostnetwork.L3VNIParams, error) {
	if underlay.Spec.SRV6 == nil {
		return []hostnetwork.L3VNIParams{}, nil
	}
	hostL3VNIs := []hostnetwork.L3VNIParams{}
	for _, l3vpn := range l3VPNs {
		hostL3VNI, err := l3vpnToHost(l3vpn, vtepIP, targetNS, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to translate L3VPN %s, err: %w", l3vpn.Name, err)
		}
		hostL3VNIs = append(hostL3VNIs, hostL3VNI)
	}
	return hostL3VNIs, nil
}

func l3vpnToHost(vni v1alpha1.L3VPN, vtepIP, targetNS string, nodeIndex int) (hostnetwork.L3VNIParams, error) {
	hostL3VNI := hostnetwork.L3VNIParams{
		VNIParams: hostnetwork.VNIParams{
			VRF:      vni.Spec.VRF,
			TargetNS: targetNS,
			VTEPIP:   vtepIP,
			VNI:      vni.Spec.RDAssignedNumber, // We use RDAssignedNumber the same as VNI in L3VNI / L2VNI.
			HasSRv6:  true,
		},
	}
	if vni.Spec.HostSession == nil {
		return hostL3VNI, nil
	}

	vethIPs, err := ipam.VethIPsFromPool(
		vni.Spec.HostSession.LocalCIDR.IPv4,
		vni.Spec.HostSession.LocalCIDR.IPv6,
		nodeIndex)
	if err != nil {
		return hostnetwork.L3VNIParams{}, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d, err: %w",
			vni.Spec.HostSession.LocalCIDR, nodeIndex, err)
	}

	hostL3VNI.HostVeth = &hostnetwork.Veth{
		HostIPv4: ipNetToString(vethIPs.Ipv4.HostSide),
		NSIPv4:   ipNetToString(vethIPs.Ipv4.PeSide),
		HostIPv6: ipNetToString(vethIPs.Ipv6.HostSide),
		NSIPv6:   ipNetToString(vethIPs.Ipv6.PeSide),
	}

	return hostL3VNI, nil
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

// ipNetToString returns the string representation of the IPNet, or empty string if IP is nil
func ipNetToString(ipNet net.IPNet) string {
	if ipNet.IP == nil {
		return ""
	}
	return ipNet.String()
}
