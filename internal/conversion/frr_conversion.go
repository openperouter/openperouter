// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"sort"
	"strings"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"k8s.io/utils/ptr"
)

const (
	isisProcessName      = "ISIS"
	locatorName          = "MAIN"
	loopbackName         = "lo"
	advertisePassiveOnly = "advertisePassiveOnly"
)

var locatorFormats = map[string]frr.SRV6Locator{
	"usid-f3216": {
		BlockLen: 32,
		NodeLen:  16,
		Behavior: "usid",
		Format:   "usid-f3216",
	},
}

type FRREmptyConfigError string

func (e FRREmptyConfigError) Error() string {
	return string(e)
}

type L3VNIOption func(*frr.L3VNIConfig) error

func WithGatewayIPs(cidrs []string) L3VNIOption {
	return func(cfg *frr.L3VNIConfig) error {
		for _, cidr := range cidrs {
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				return fmt.Errorf("failed to parse L2 gateway CIDR %s: %w", cidr, err)
			}
			prefix := ipnet.String()
			if ipfamily.ForCIDR(ipnet) == ipfamily.IPv4 {
				cfg.ToAdvertiseIPv4 = append(cfg.ToAdvertiseIPv4, prefix)
			}
			if ipfamily.ForCIDR(ipnet) == ipfamily.IPv6 {
				cfg.ToAdvertiseIPv6 = append(cfg.ToAdvertiseIPv6, prefix)
			}
		}
		return nil
	}
}

func APItoFRR(config APIConfigData, nodeIndex int, logLevel string) (frr.Config, error) {
	rawSnippets := rawConfigSnippets(config.RawFRRConfigs)
	if len(rawSnippets) > 0 && len(config.Underlays) == 0 {
		slog.Info("no underlay provided, applying raw configuration only")
		return frr.Config{
			Loglevel:  logLevel,
			RawConfig: rawSnippets,
		}, nil
	}

	// Common validation between the FRR and Host config conversion layer.
	if err := validateAPIConfigData(config); err != nil {
		return frr.Config{}, err
	}

	underlay := config.Underlays[0]

	routerID, err := routerIDFromUnderlay(underlay, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to get routerID: %w", err)
	}

	tunnelEndpoint, err := tunnelEndpointToFRR(underlay, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to translate tunnel endpoint settings, err: %w", err)
	}

	underlayConfigISIS, err := isisToFRR(underlay.Spec.ISIS, underlay.Spec.Nics, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to translate ISIS settings, err: %w", err)
	}

	underlayConfigSegmentRouting, err := segmentRoutingToFRR(underlay, nodeIndex, tunnelEndpoint)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to translate segment routing settings, err: %w", err)
	}

	neighbors, err := neighborsToFRR(
		underlay.Spec.Neighbors,
		underlayConfigSegmentRouting,
		config.L2VNIs,
		config.L3VNIs,
		config.L3VPNs,
		config.L3Passthrough,
	)
	if err != nil {
		return frr.Config{}, err
	}

	underlayConfig := frr.UnderlayConfig{
		MyASN:          underlay.Spec.ASN,
		RouterID:       routerID,
		Neighbors:      neighbors,
		TunnelEndpoint: tunnelEndpoint,
		ISIS:           underlayConfigISIS,
		SegmentRouting: underlayConfigSegmentRouting,
	}

	applyGracefulRestart(&underlayConfig, underlay.Spec.GracefulRestart)

	vniConfigs, err := vniConfigsToFRR(config.L3VNIs, config.L2VNIs, routerID, underlay.Spec.ASN, nodeIndex)
	if err != nil {
		return frr.Config{}, err
	}

	passthroughConfig, err := passthroughToFRR(config.L3Passthrough, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to translate passthrough to frr: %w", err)
	}

	vpnConfigs, err := l3vpnConfigsToFRR(underlay, config.L3VPNs, routerID, nodeIndex)
	if err != nil {
		return frr.Config{}, err
	}

	return frr.Config{
		Underlay:    underlayConfig,
		VNIs:        vniConfigs,
		Passthrough: passthroughConfig,
		BFDProfiles: bfdProfilesFromNeighbors(underlay.Spec.Neighbors),
		VPNs:        vpnConfigs,
		Loglevel:    logLevel,
		RawConfig:   rawSnippets,
	}, nil
}

func neighborsToFRR(apiNeighbors []v1alpha1.Neighbor, segmentRouting *frr.UnderlaySegmentRouting,
	l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN, l3passthroughs []v1alpha1.L3Passthrough,
) ([]frr.NeighborConfig, error) {
	neighbors := make([]frr.NeighborConfig, 0, len(apiNeighbors))
	for _, n := range apiNeighbors {
		frrNeigh, err := neighborToFRR(n, segmentRouting, l2vnis, l3vnis, l3vpns, l3passthroughs)
		if err != nil {
			return nil, fmt.Errorf("failed to translate underlay neighbor %s to frr, err: %w", neighborID(n), err)
		}
		neighbors = append(neighbors, *frrNeigh)
	}
	return neighbors, nil
}

func bfdProfilesFromNeighbors(apiNeighbors []v1alpha1.Neighbor) []frr.BFDProfile {
	profiles := []frr.BFDProfile{}
	for _, n := range apiNeighbors {
		if p := bfdProfileForNeighbor(n); p != nil {
			profiles = append(profiles, *p)
		}
	}
	return profiles
}

func applyGracefulRestart(config *frr.UnderlayConfig, gr *v1alpha1.GracefulRestartConfig) {
	if gr == nil {
		return
	}
	config.GracefulRestart = &frr.GracefulRestart{
		RestartTime:   ptr.Deref(gr.RestartTimeSeconds, 120),
		StalePathTime: ptr.Deref(gr.StalePathTimeSeconds, 360),
	}
	const grConnectRetrySeconds = int64(5)
	for i := range config.Neighbors {
		if config.Neighbors[i].ConnectTime == nil {
			config.Neighbors[i].ConnectTime = new(grConnectRetrySeconds)
		}
	}
}

func tunnelEndpointToFRR(underlay v1alpha1.Underlay, nodeIndex int) (*frr.TunnelEndpoint, error) {
	if underlay.Spec.TunnelEndpoint == nil {
		return nil, nil
	}
	tunnelEndpoint := &frr.TunnelEndpoint{}
	for _, cidr := range underlay.Spec.TunnelEndpoint.CIDRs {
		af := ipfamily.ForCIDRString(cidr)
		if af == ipfamily.Unknown {
			return nil, fmt.Errorf("failed to determine address family for CIDR %q", cidr)
		}

		ip, err := ipam.VTEPIp(cidr, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w", cidr, nodeIndex, err)
		}

		if af == ipfamily.IPv4 {
			tunnelEndpoint.IPv4CIDR = ip.String()
			continue
		}
		tunnelEndpoint.IPv6CIDR = ip.String()
	}
	return tunnelEndpoint, nil
}

func vniConfigsToFRR(l3vnis []v1alpha1.L3VNI, l2vnis []v1alpha1.L2VNI, routerID string, underlayASN int64, nodeIndex int) ([]frr.L3VNIConfig, error) {
	vrfsWithL2Gateway := vrfsWithL2Gateways(l2vnis)
	configs := []frr.L3VNIConfig{}
	for _, vni := range l3vnis {
		var opts []L3VNIOption
		if gatewayCIDRs, ok := vrfsWithL2Gateway[vni.Spec.VRF]; ok {
			opts = append(opts, WithGatewayIPs(gatewayCIDRs))
		}
		frrVNI, err := l3vniToFRR(vni, routerID, underlayASN, nodeIndex, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to translate vni to frr: %w, vni %v", err, vni)
		}
		configs = append(configs, frrVNI...)
	}
	return configs, nil
}

func segmentRoutingToFRR(underlay v1alpha1.Underlay, nodeIndex int, tunnelEndpoint *frr.TunnelEndpoint) (*frr.UnderlaySegmentRouting, error) {
	if underlay.Spec.SRV6 == nil {
		return nil, nil
	}
	if tunnelEndpoint == nil || tunnelEndpoint.IPv6CIDR == "" {
		return nil, fmt.Errorf("SRv6 Source CIDR must be set")
	}

	configs := underlay.Spec.SRV6

	locator, isValid := locatorFormats[configs.Locator.Format]
	if !isValid {
		return nil, fmt.Errorf("invalid locator format %q", configs.Locator.Format)
	}
	locator.Name = locatorName

	var err error
	if locator.Prefix, err = ipam.OffsetPrefix(
		configs.Locator.BasePrefix,
		nodeIndex,
		locator.BlockLen+locator.NodeLen); err != nil {
		return nil, fmt.Errorf("could not calculate SRV6 prefix for node, %w", err)
	}

	ip, _, _ := strings.Cut(tunnelEndpoint.IPv6CIDR, "/")

	return &frr.UnderlaySegmentRouting{
		SourceAddress: ip,
		Locator:       locator,
	}, nil
}

func isisToFRR(isisConfig *v1alpha1.ISISConfig, nics []string, nodeIndex int) (*frr.UnderlayISIS, error) {
	if isisConfig == nil {
		return nil, nil
	}

	isisLevel := ptr.Deref(isisConfig.Level, 0)
	if isisLevel > 2 {
		return nil, fmt.Errorf("ISIS level invalid, must be 1, 2 or unset")
	}

	net, err := frr.ParseISISNet(isisConfig.BaseNet)
	if err != nil {
		return nil, fmt.Errorf("ISIS net address invalid, err: %w", err)
	}

	newSystemID, err := frr.IncrementSystemID(net.SystemID, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("could not increment ISIS systemID, err: %w", err)
	}
	net.SystemID = newSystemID

	// Always add the loopback as an IPv6 only and passive interface (for advertisePassiveOnly).
	isisInterfaces := map[string]frr.ISISInterface{
		loopbackName: {
			Name:      loopbackName,
			IPv6:      true,
			IsPassive: true,
		},
	}

	// Add underlay.Spec.Nics as IPv6 only, non-passive interfaces.
	for _, nic := range nics {
		isisInterfaces[nic] = frr.ISISInterface{
			Name: nic,
			IPv6: true,
		}
	}

	// Build interfaces and make sure that they are unique in the list. The ISISInterface slice may override default
	// settings from loopback and from nics.
	interfaceNames := make(map[string]struct{}, len(isisConfig.Interfaces))
	for _, intf := range isisConfig.Interfaces {
		name := intf.Name
		if _, alreadySet := interfaceNames[name]; alreadySet {
			return nil, fmt.Errorf("ISIS interfaces invalid, duplicate interface name %s", name)
		}
		interfaceNames[name] = struct{}{}

		isIPv4 := intf.IPFamily != nil &&
			(*intf.IPFamily == v1alpha1.IPFamilyIPv4 || *intf.IPFamily == v1alpha1.IPFamilyDualStack)
		isIPv6 := intf.IPFamily == nil ||
			*intf.IPFamily == v1alpha1.IPFamilyIPv6 || *intf.IPFamily == v1alpha1.IPFamilyDualStack

		isisInterfaces[intf.Name] = frr.ISISInterface{
			Name: intf.Name,
			IPv4: isIPv4,
			IPv6: isIPv6,
		}
	}

	interfaces := slices.Collect(maps.Values(isisInterfaces))
	slices.SortFunc(interfaces, func(x, y frr.ISISInterface) int {
		if x.Name > y.Name {
			return 1
		}
		if x.Name == y.Name {
			return 0
		}
		return -1
	})

	return &frr.UnderlayISIS{
		Name:                 isisProcessName,
		Net:                  net,
		Level:                isisLevel,
		AdvertisePassiveOnly: slices.Contains(isisConfig.Features, advertisePassiveOnly),
		Interfaces:           interfaces,
	}, nil
}

func rawConfigSnippets(rawFRRConfigs []v1alpha1.RawFRRConfig) []frr.RawFRRSnippet {
	if len(rawFRRConfigs) == 0 {
		return nil
	}
	snippets := make([]frr.RawFRRSnippet, 0, len(rawFRRConfigs))
	for _, rc := range rawFRRConfigs {
		snippets = append(snippets, frr.RawFRRSnippet{
			Priority: rc.Spec.Priority,
			Config:   rc.Spec.RawConfig,
		})
	}
	sort.SliceStable(snippets, func(i, j int) bool {
		return ptr.Deref(snippets[i].Priority, 0) < ptr.Deref(snippets[j].Priority, 0)
	})
	return snippets
}

func passthroughToFRR(l3Passthroughs []v1alpha1.L3Passthrough, nodeIndex int) (*frr.PassthroughConfig, error) {
	if len(l3Passthroughs) == 0 {
		return nil, nil
	}
	passthrough := l3Passthroughs[0]

	vethIPs, err := ipam.VethIPsFromPool(passthrough.Spec.HostSession.LocalCIDR.IPv4, passthrough.Spec.HostSession.LocalCIDR.IPv6, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get veth ips, cidr %v, nodeIndex %d", passthrough.Spec.HostSession.LocalCIDR, nodeIndex)
	}

	res := &frr.PassthroughConfig{
		ToAdvertiseIPv4: []string{},
		ToAdvertiseIPv6: []string{},
	}
	asn, err := frr.NewPeerASN(
		passthrough.Spec.HostSession.HostASN,
		passthrough.Spec.HostSession.HostType,
	)
	if err != nil {
		return nil, fmt.Errorf("could not parse passthrough HostSession, err: %w", err)
	}

	if vethIPs.Ipv4.HostSide.IP != nil {
		res.LocalNeighborV4 = &frr.NeighborConfig{
			ASN:  asn,
			Addr: vethIPs.Ipv4.HostSide.IP.String(),
			ID:   vethIPs.Ipv4.HostSide.IP.String(),
		}
		ipnet := net.IPNet{
			IP:   vethIPs.Ipv4.HostSide.IP,
			Mask: net.CIDRMask(32, 32),
		}

		res.ToAdvertiseIPv4 = append(res.ToAdvertiseIPv4, ipnet.String())
	}
	if vethIPs.Ipv6.HostSide.IP != nil {
		res.LocalNeighborV6 = &frr.NeighborConfig{
			ASN:  asn,
			Addr: vethIPs.Ipv6.HostSide.IP.String(),
			ID:   vethIPs.Ipv6.HostSide.IP.String(),
		}

		ipnet := net.IPNet{
			IP:   vethIPs.Ipv6.HostSide.IP,
			Mask: net.CIDRMask(128, 128),
		}
		res.ToAdvertiseIPv6 = append(res.ToAdvertiseIPv6, ipnet.String())
	}

	return res, nil
}

// l3vniToFRR converts an L3VNI CR into one or two FRR L3VNIConfigs.
// If no HostSession is defined, it returns a single config using the underlay ASN.
// Otherwise, it derives veth IPs from the HostSession's local CIDR pool for the given node index
// and creates a config per IP family (IPv4/IPv6), each with a local neighbor and the corresponding prefixes to advertise.
func l3vniToFRR(vni v1alpha1.L3VNI, routerID string, underlayASN int64, nodeIndex int, opts ...L3VNIOption) ([]frr.L3VNIConfig, error) {
	if vni.Spec.HostSession == nil { // no neighbor, just the vni / vrf
		cfg := frr.L3VNIConfig{
			VNI:       vni.Spec.VNI,
			VRF:       vni.Spec.VRF,
			ASN:       underlayASN, // Since there is no session, the ASN is arbitrary
			RouterID:  routerID,
			ExportRTs: vni.Spec.ExportRTs,
			ImportRTs: vni.Spec.ImportRTs,
		}
		for _, opt := range opts {
			if err := opt(&cfg); err != nil {
				return nil, err
			}
		}
		return []frr.L3VNIConfig{cfg}, nil
	}

	hostASN, err := frr.NewPeerASN(vni.Spec.HostSession.HostASN, vni.Spec.HostSession.HostType)
	if err != nil {
		return nil, fmt.Errorf("could not parse HostSession, err: %w", err)
	}

	veths, err := ipam.VethIPsFromPool(vni.Spec.HostSession.LocalCIDR.IPv4, vni.Spec.HostSession.LocalCIDR.IPv6, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get veths ips for vni %s: %w", vni.Name, err)
	}

	hostSideIPs := []net.IPNet{}
	if ip := veths.Ipv4.HostSide.IP; ip != nil {
		hostSideIPs = append(hostSideIPs, net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)})
	}
	if ip := veths.Ipv6.HostSide.IP; ip != nil {
		hostSideIPs = append(hostSideIPs, net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)})
	}
	if len(hostSideIPs) == 0 {
		return nil, fmt.Errorf("no valid host side IP found for vni %s", vni.Name)
	}

	configs := []frr.L3VNIConfig{}
	for _, ipnet := range hostSideIPs {
		toAdvertiseIPv4, toAdvertiseIPv6 := []string{}, []string{}
		if ipfamily.ForCIDR(&ipnet) == ipfamily.IPv4 {
			toAdvertiseIPv4 = []string{ipnet.String()}
		} else {
			toAdvertiseIPv6 = []string{ipnet.String()}
		}

		configs = append(configs, frr.L3VNIConfig{
			ASN:      vni.Spec.HostSession.ASN,
			VNI:      vni.Spec.VNI,
			VRF:      vni.Spec.VRF,
			RouterID: routerID,
			LocalNeighbor: &frr.NeighborConfig{
				Addr: ipnet.IP.String(),
				ID:   ipnet.IP.String(),
				ASN:  hostASN,
			},
			ExportRTs:       vni.Spec.ExportRTs,
			ImportRTs:       vni.Spec.ImportRTs,
			ToAdvertiseIPv4: toAdvertiseIPv4,
			ToAdvertiseIPv6: toAdvertiseIPv6,
		})
	}
	for i := range configs {
		for _, opt := range opts {
			if err := opt(&configs[i]); err != nil {
				return nil, err
			}
		}
	}
	return configs, nil
}

func l3vpnConfigsToFRR(underlay v1alpha1.Underlay, l3VPNs []v1alpha1.L3VPN, routerID string,
	nodeIndex int) ([]frr.L3VPNConfig, error) {
	vpnConfigs := []frr.L3VPNConfig{}
	for _, vpn := range l3VPNs {
		frrVNI, err := l3vpnToFRR(vpn, routerID, underlay.Spec.ASN, nodeIndex)
		if err != nil {
			return []frr.L3VPNConfig{}, fmt.Errorf("failed to translate l3vpn to frr: %w, vni %v", err, vpn)
		}
		vpnConfigs = append(vpnConfigs, frrVNI...)
	}
	return vpnConfigs, nil
}

// l3vpnToFRR converts an L3VPN CR into one or two FRR L3VPNConfigs.
// If no HostSession is defined, it returns a single config using the underlay ASN.
// Otherwise, it derives veth IPs from the HostSession's local CIDR pool for the given node index
// and creates a config per IP family (IPv4/IPv6), each with a local neighbor and the corresponding prefixes to
// advertise.
func l3vpnToFRR(vpn v1alpha1.L3VPN, routerID string, underlayASN int64, nodeIndex int) ([]frr.L3VPNConfig, error) {
	exportRTs, err := convertRTsWithDefault(vpn.Spec.ExportRTs, underlayASN, vpn.Spec.RDAssignedNumber)
	if err != nil {
		return nil, fmt.Errorf("could not convert export Route Targets for L3VPN %s, err: %w", vpn.Name, err)
	}
	importRTs, err := convertRTs(vpn.Spec.ImportRTs)
	if err != nil {
		return nil, fmt.Errorf("could not convert import Route Targets for L3VPN %s, err: %w", vpn.Name, err)
	}

	// importRTs cannot be auto-derived. Unfortunately, FRR does not support wildcard notation, e.g. *:200. And
	// using 0, e.g. 0:200, imports the route target verbatim.
	if len(importRTs) < 1 {
		return nil, errors.New("invalid configuration for importRTs, must provide at least one explicit import Route Target")
	}

	if vpn.Spec.HostSession == nil { // no neighbor, just the vni / vrf
		return []frr.L3VPNConfig{
			{
				ASN:                underlayASN, // Since there is no session, the ASN is arbitrary
				VRF:                vpn.Spec.VRF,
				RouterID:           routerID,
				ExportRTs:          exportRTs,
				ImportRTs:          importRTs,
				RouteDistinguisher: routeDistinguisher(routerID, vpn.Spec.RDAssignedNumber),
			},
		}, nil
		// TODO: check if opts is needed, the same as for L3VNI.
	}

	hostASN, err := frr.NewPeerASN(vpn.Spec.HostSession.HostASN, vpn.Spec.HostSession.HostType)
	if err != nil {
		return nil, fmt.Errorf("could not parse HostSession, err: %w", err)
	}

	veths, err := ipam.VethIPsFromPool(vpn.Spec.HostSession.LocalCIDR.IPv4, vpn.Spec.HostSession.LocalCIDR.IPv6, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get veths ips for vni %s: %w", vpn.Name, err)
	}

	hostSideIPs := []net.IPNet{}
	if ip := veths.Ipv4.HostSide.IP; ip != nil {
		hostSideIPs = append(hostSideIPs, net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)})
	}
	if ip := veths.Ipv6.HostSide.IP; ip != nil {
		hostSideIPs = append(hostSideIPs, net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)})
	}
	if len(hostSideIPs) == 0 {
		return nil, fmt.Errorf("no valid host side IP found for vni %s", vpn.Name)
	}

	configs := []frr.L3VPNConfig{}
	for _, ipnet := range hostSideIPs {
		toAdvertiseIPv4, toAdvertiseIPv6 := []string{}, []string{}
		if ipfamily.ForCIDR(&ipnet) == ipfamily.IPv4 {
			toAdvertiseIPv4 = []string{ipnet.String()}
		} else {
			toAdvertiseIPv6 = []string{ipnet.String()}
		}

		configs = append(configs, frr.L3VPNConfig{
			ASN:                vpn.Spec.HostSession.ASN,
			ExportRTs:          exportRTs,
			ImportRTs:          importRTs,
			RouteDistinguisher: routeDistinguisher(routerID, vpn.Spec.RDAssignedNumber),
			VRF:                vpn.Spec.VRF,
			RouterID:           routerID,
			LocalNeighbor: &frr.NeighborConfig{
				Addr: ipnet.IP.String(),
				ID:   ipnet.IP.String(),
				ASN:  hostASN,
			},
			ToAdvertiseIPv4: toAdvertiseIPv4,
			ToAdvertiseIPv6: toAdvertiseIPv6,
		})
	}
	// TODO: check if opts is needed, the same as for L3VNI.

	return configs, nil
}

func routeDistinguisher(left string, right int32) string {
	return fmt.Sprintf("%s:%d", left, right)
}

// convertRTsWithDefault converts the provided routeTarget []v1alpha1.RouteTarget to a space separated string. If
// routeTarget is empty, return a default route target which is built by concatenating asn and rdAssignedNumber. This
// default is for exportRTs only, and this should be called with asn=<under lay ASN>, so that we get the auto-generated
// RT <asn>:<rdAssignedNumber>.
func convertRTsWithDefault(routeTargets []v1alpha1.RouteTarget, asn int64, rdAssignedNumber int32) (string, error) {
	if len(routeTargets) == 0 {
		return fmt.Sprintf("%d:%d", asn, rdAssignedNumber), nil
	}
	return convertRTs(routeTargets)
}

// convertRTs converts the provided routeTarget []v1alpha1.RouteTarget to a space separated string.
func convertRTs(routeTargets []v1alpha1.RouteTarget) (string, error) {
	str := make([]string, 0, len(routeTargets))
	for _, rt := range routeTargets {
		if err := validateRouteTarget(string(rt)); err != nil {
			return "", err
		}
		str = append(str, string(rt))
	}
	return strings.Join(str, " "), nil
}

func neighborToFRR(n v1alpha1.Neighbor, segmentRouting *frr.UnderlaySegmentRouting,
	l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN, l3passthroughs []v1alpha1.L3Passthrough,
) (*frr.NeighborConfig, error) {
	asn, err := frr.NewPeerASN(n.ASN, n.Type)
	if err != nil {
		return nil, fmt.Errorf("neighbor %s: could not parse ASN configuration, err: %w", neighborID(n), err)
	}

	neighName := neighborName(asn, neighborID(n))

	ipFamilies, err := ipFamiliesForNeighbor(n, neighName, l2vnis, l3vnis, l3vpns, l3passthroughs)
	if err != nil {
		return nil, err
	}

	var updateSource string
	if segmentRouting != nil &&
		(slices.Contains(ipFamilies, ipfamily.AfiSafi{AFI: ipfamily.IPv4, SAFI: ipfamily.VPN}) ||
			slices.Contains(ipFamilies, ipfamily.AfiSafi{AFI: ipfamily.IPv6, SAFI: ipfamily.VPN})) {
		updateSource = segmentRouting.SourceAddress
	}

	res := &frr.NeighborConfig{
		Name:         neighName,
		ASN:          asn,
		Addr:         ptr.Deref(n.Address, ""),
		Interface:    ptr.Deref(n.Interface, ""),
		Port:         n.Port,
		EBGPMultiHop: ptr.Deref(n.EBGPMultiHop, false),
		Password:     ptr.Deref(n.Password, ""),
		UpdateSource: updateSource,
		IPFamilies:   ipFamilies,
	}

	if err := validateNeighborConfig(res); err != nil {
		return nil, err
	}

	setIDForNeighbor(res)

	if err := setExtendedNexthopForNeighbor(res); err != nil {
		return nil, err
	}

	res.HoldTime = n.HoldTimeSeconds
	res.KeepaliveTime = n.KeepaliveTimeSeconds
	res.ConnectTime = n.ConnectTimeSeconds

	if n.BFD == nil {
		return res, nil
	}

	res.BFDEnabled = true
	if ptr.AllPtrFieldsNil(n.BFD) {
		return res, nil
	}
	res.BFDProfile = bfdProfileNameForNeighbor(n)

	return res, nil
}

func validateNeighborConfig(res *frr.NeighborConfig) error {
	if res.Addr == "" && res.Interface == "" {
		return fmt.Errorf("either a neighbor Address or an Interface must be configured")
	}
	if res.Addr != "" && res.Interface != "" {
		return fmt.Errorf("neighbor Address and neighbor Interface are mutually exclusive")
	}
	return nil
}

func setIDForNeighbor(res *frr.NeighborConfig) {
	if res.Addr != "" {
		res.ID = res.Addr
		return
	}
	res.ID = res.Interface
}

func setExtendedNexthopForNeighbor(res *frr.NeighborConfig) error {
	if res.Interface != "" {
		res.ExtendedNexthop = true
		return nil
	}

	neighborFamily, err := ipfamily.ForAddresses(res.Addr)
	if err != nil {
		return fmt.Errorf("failed to find ipfamily for %s, %w", res.Addr, err)
	}
	if neighborFamily != ipfamily.IPv4 {
		res.ExtendedNexthop = true
	}
	return nil
}

// ipFamiliesForNeighbor converts API neighbor IP families to internal representations. If n.AddressFamilies is
// empty, it chooses sane defaults according to the rules stipulated on v1alpha1.Neighbor.AddressFamilies.
func ipFamiliesForNeighbor(n v1alpha1.Neighbor, neighName string,
	l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN, l3passthroughs []v1alpha1.L3Passthrough,
) ([]ipfamily.AfiSafi, error) {

	addressFamilies := n.AddressFamilies
	if len(addressFamilies) == 0 {
		addressFamilies = defaultFamiliesForNeighbor(n, l2vnis, l3vnis, l3vpns, l3passthroughs)
	}

	families := make([]ipfamily.AfiSafi, 0, len(addressFamilies))
	for _, af := range addressFamilies {
		switch af.Type {
		case "ipv4unicast":
			families = append(families, ipfamily.AfiSafi{AFI: ipfamily.IPv4, SAFI: ipfamily.Unicast})
		case "ipv6unicast":
			families = append(families, ipfamily.AfiSafi{AFI: ipfamily.IPv6, SAFI: ipfamily.Unicast})
		case "evpn":
			families = append(families, ipfamily.AfiSafi{AFI: ipfamily.L2VPN, SAFI: ipfamily.EVPN})
		case "ipv4vpn":
			families = append(families, ipfamily.AfiSafi{AFI: ipfamily.IPv4, SAFI: ipfamily.VPN})
		case "ipv6vpn":
			families = append(families, ipfamily.AfiSafi{AFI: ipfamily.IPv6, SAFI: ipfamily.VPN})
		default:
			return nil, fmt.Errorf("neighbor %s: unsupported address family type %q", neighName, af.Type)
		}
	}
	return families, nil
}

func defaultFamiliesForNeighbor(n v1alpha1.Neighbor,
	l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN, l3passthroughs []v1alpha1.L3Passthrough,
) []v1alpha1.NeighborAddressFamily {
	addIPv4Unicast := false
	addIPv6Unicast := false
	addEVPN := false
	addIPv4VPN := false
	addIPv6VPN := false

	intf := ptr.Deref(n.Interface, "")
	address := net.ParseIP(ptr.Deref(n.Address, ""))
	isIPv6Neighbor := intf == "" && ipfamily.ForAddress(address) == ipfamily.IPv6

	if !isIPv6Neighbor {
		addIPv4Unicast = true
	}

	if isIPv6Neighbor {
		addIPv6Unicast = true
	}

	for _, l3passthrough := range l3passthroughs {
		if ptr.Deref(l3passthrough.Spec.HostSession.LocalCIDR.IPv4, "") != "" {
			addIPv4Unicast = true
		}
		if ptr.Deref(l3passthrough.Spec.HostSession.LocalCIDR.IPv6, "") != "" {
			addIPv6Unicast = true
		}
	}

	if len(l2vnis) > 0 || len(l3vnis) > 0 {
		addIPv4Unicast = true
		addEVPN = true
	}

	if isIPv6Neighbor && len(l3vpns) > 0 {
		addIPv4VPN = true
		addIPv6VPN = true
	}

	defaultFamilies := []v1alpha1.NeighborAddressFamily{}
	if addIPv4Unicast {
		defaultFamilies = append(defaultFamilies, v1alpha1.NeighborAddressFamily{Type: "ipv4unicast"})
	}
	if addIPv6Unicast {
		defaultFamilies = append(defaultFamilies, v1alpha1.NeighborAddressFamily{Type: "ipv6unicast"})
	}
	if addEVPN {
		defaultFamilies = append(defaultFamilies, v1alpha1.NeighborAddressFamily{Type: "evpn"})
	}
	if addIPv4VPN {
		defaultFamilies = append(defaultFamilies, v1alpha1.NeighborAddressFamily{Type: "ipv4vpn"})
	}
	if addIPv6VPN {
		defaultFamilies = append(defaultFamilies, v1alpha1.NeighborAddressFamily{Type: "ipv6vpn"})
	}
	return defaultFamilies
}

func bfdProfileForNeighbor(n v1alpha1.Neighbor) *frr.BFDProfile {
	if n.BFD == nil {
		return nil
	}

	if ptr.AllPtrFieldsNil(n.BFD) {
		return nil
	}

	profileName := bfdProfileNameForNeighbor(n)
	bfdProfile := &frr.BFDProfile{
		Name:             profileName,
		ReceiveInterval:  n.BFD.ReceiveInterval,
		TransmitInterval: n.BFD.TransmitInterval,
		DetectMultiplier: n.BFD.DetectMultiplier,
		EchoInterval:     n.BFD.EchoInterval,
		EchoMode:         ptr.Deref(n.BFD.EchoMode, false),
		PassiveMode:      ptr.Deref(n.BFD.PassiveMode, false),
		MinimumTTL:       n.BFD.MinimumTTL,
	}

	return bfdProfile
}

func neighborID(n v1alpha1.Neighbor) string {
	if address := ptr.Deref(n.Address, ""); address != "" {
		return address
	}
	return ptr.Deref(n.Interface, "")
}

func bfdProfileNameForNeighbor(n v1alpha1.Neighbor) string {
	return fmt.Sprintf("neighbor-%s", neighborID(n))
}

func neighborName(asn frr.PeerASN, id string) string {
	return fmt.Sprintf("%s@%s", asn, id)
}

func routerIDFromUnderlay(underlay v1alpha1.Underlay, nodeIndex int) (string, error) {
	// RouterIDCIDR defaults are applied via CRD schema, so it should always be set
	routerIDCidr := ptr.Deref(underlay.Spec.RouterIDCIDR, "10.0.0.0/24")
	routerID, err := ipam.RouterID(routerIDCidr, nodeIndex)
	if err != nil {
		return "", fmt.Errorf("failed to get router id, cidr %s, nodeIndex %d: %w", routerIDCidr, nodeIndex, err)
	}
	return routerID, nil
}

func vrfsWithL2Gateways(l2vnis []v1alpha1.L2VNI) map[string][]string {
	res := make(map[string][]string)
	for _, l2vni := range l2vnis {
		if len(l2vni.Spec.L2GatewayIPs) > 0 {
			res[*l2vni.Spec.VRF] = l2vni.Spec.L2GatewayIPs
		}
	}
	return res
}
