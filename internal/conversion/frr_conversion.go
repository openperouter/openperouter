// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"sort"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"github.com/openperouter/openperouter/internal/networklayerprotocol"
	"k8s.io/utils/ptr"
)

const (
	isisProcessName      = "ISIS"
	locatorName          = "MAIN"
	loopbackName         = "lo"
	advertisePassiveOnly = "advertisePassiveOnly"
	passiveInterface     = "passive"
)

var locatorFormats = map[string]frr.SRV6Locator{
	"usid-f3216": {
		BlockLen: 32,
		NodeLen:  16,
		Behavior: "usid",
		Format:   "usid-f3216",
	},
}

type NoUnderlaysError string

func (e NoUnderlaysError) Error() string {
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

	tunnelEndpoint, err := tunnelEndpointToFRR(underlay.Spec.TunnelEndpoint, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to translate tunnel endpoint settings, err: %w", err)
	}

	underlayInterfaces, err := underlayNetworkDeviceInterfaceNames(underlay.Spec.Interfaces)
	if err != nil {
		return frr.Config{}, err
	}

	underlayConfigISIS, err := underlayISISToFRR(underlay.Spec.ISIS, underlayInterfaces, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to translate ISIS settings, err: %w", err)
	}

	underlayConfigSegmentRouting, err := underlaySegmentRoutingToFRR(underlay.Spec.SRV6, nodeIndex, tunnelEndpoint)
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
		underlay.Spec.TunnelEndpoint,
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

	vpnConfigs, err := l3vpnConfigsToFRR(config.L3VPNs, routerID, underlay.Spec.ASN, nodeIndex)
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
	tunnelEndpoint *v1alpha1.TunnelEndpointConfig,
) ([]frr.NeighborConfig, error) {
	neighbors := make([]frr.NeighborConfig, 0, len(apiNeighbors))
	for _, n := range apiNeighbors {
		frrNeigh, err := neighborToFRR(
			n,
			l2vnis,
			l3vnis,
			l3vpns,
			l3passthroughs,
			tunnelEndpoint,
			segmentRouting,
		)
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

func tunnelEndpointToFRR(tunnelEndpointConfig *v1alpha1.TunnelEndpointConfig, nodeIndex int) (*frr.TunnelEndpoint, error) {
	if tunnelEndpointConfig == nil {
		return nil, nil
	}
	tunnelEndpoint := &frr.TunnelEndpoint{}
	for _, cidr := range tunnelEndpointConfig.CIDRs {
		af := ipfamily.ForCIDRString(cidr)
		if af == ipfamily.Unknown {
			return nil, fmt.Errorf("failed to determine address family for CIDR %q", cidr)
		}

		ip, err := ipam.TunnelEndpointIP(cidr, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w", cidr, nodeIndex, err)
		}

		if af == ipfamily.IPv4 {
			tunnelEndpoint.IPv4CIDR = ip.String()
			continue
		}
		tunnelEndpoint.IPv6CIDR = ip.String()
	}
	if tunnelEndpoint.IPv4CIDR == "" && tunnelEndpoint.IPv6CIDR == "" {
		return nil, fmt.Errorf("no tunnel endpoint IP available after conversion from CIDRs: %v",
			tunnelEndpointConfig.CIDRs)
	}
	return tunnelEndpoint, nil
}

func vniConfigsToFRR(
	l3vnis []v1alpha1.L3VNI,
	l2vnis []v1alpha1.L2VNI,
	routerID string,
	underlayASN int64,
	nodeIndex int,
) ([]frr.L3VNIConfig, error) {
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

func underlaySegmentRoutingToFRR(srv6Config *v1alpha1.SRV6Config, nodeIndex int, tunnelEndpoint *frr.TunnelEndpoint) (*frr.UnderlaySegmentRouting, error) {
	if srv6Config == nil {
		return nil, nil
	}
	if tunnelEndpoint == nil || tunnelEndpoint.IPv6CIDR == "" {
		return nil, fmt.Errorf("SRv6 Source CIDR must be set")
	}

	locator, isValid := locatorFormats[srv6Config.Locator.Format]
	if !isValid {
		return nil, fmt.Errorf("invalid locator format %q", srv6Config.Locator.Format)
	}
	locator.Name = locatorName

	var err error
	locator.Prefix, err = ipam.OffsetWithPrefix(
		srv6Config.Locator.BasePrefix,
		nodeIndex,
		locator.BlockLen+locator.NodeLen)
	if err != nil {
		return nil, fmt.Errorf("could not calculate SRV6 prefix for node, %w", err)
	}

	ip, _, err := net.ParseCIDR(tunnelEndpoint.IPv6CIDR)
	if err != nil {
		return nil, fmt.Errorf("could not parse tunnel endpoint IPv6CIDR, %w", err)
	}

	return &frr.UnderlaySegmentRouting{
		SourceAddress: ip.String(),
		Locator:       locator,
	}, nil
}

func underlayISISToFRR(isisConfig *v1alpha1.ISISConfig, interfaces []string, nodeIndex int) (*frr.UnderlayISIS, error) {
	if isisConfig == nil {
		return nil, nil
	}

	isisLevel := ptr.Deref(isisConfig.Level, 0)
	if isisLevel > 2 {
		return nil, fmt.Errorf("ISIS level invalid, must be 1, 2 or unset")
	}

	baseISISNet, err := frr.ParseISISNet(isisConfig.BaseNet)
	if err != nil {
		return nil, fmt.Errorf("ISIS net address invalid, err: %w", err)
	}

	systemID, err := frr.IncrementSystemID(baseISISNet.SystemID, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("could not increment ISIS systemID, err: %w", err)
	}
	isisNet := baseISISNet
	isisNet.SystemID = systemID

	// Always add the loopback as an IPv6 only and passive interface (for advertisePassiveOnly).
	isisInterfaces := map[string]frr.ISISInterface{
		loopbackName: {
			Name:      loopbackName,
			IPv6:      true,
			IsPassive: true,
		},
	}

	// Add underlay.Spec.Interfaces as IPv6 only, non-passive interfaces.
	for _, iface := range interfaces {
		isisInterfaces[iface] = frr.ISISInterface{
			Name: iface,
			IPv6: true,
		}
	}

	// The ISISInterface slice may override default settings from loopback and
	// from interfaces. CEL enforces uniqueness by name.
	for _, intf := range isisConfig.Interfaces {
		hasIPv4 := intf.IPFamily != nil &&
			(*intf.IPFamily == v1alpha1.IPFamilyIPv4 || *intf.IPFamily == v1alpha1.IPFamilyDualStack)
		hasIPv6 := intf.IPFamily != nil &&
			(*intf.IPFamily == v1alpha1.IPFamilyIPv6 || *intf.IPFamily == v1alpha1.IPFamilyDualStack)

		isisInterfaces[intf.Name] = frr.ISISInterface{
			Name:      intf.Name,
			IPv4:      hasIPv4,
			IPv6:      hasIPv6,
			IsPassive: slices.Contains(intf.Features, passiveInterface),
		}
	}

	return &frr.UnderlayISIS{
		Name:                 isisProcessName,
		Net:                  isisNet,
		Level:                isisLevel,
		AdvertisePassiveOnly: slices.Contains(isisConfig.Features, advertisePassiveOnly),
		Interfaces:           mapOfInterfacesToSortedList(isisInterfaces),
	}, nil
}

func mapOfInterfacesToSortedList(m map[string]frr.ISISInterface) []frr.ISISInterface {
	s := slices.Collect(maps.Values(m))
	slices.SortFunc(s, func(x, y frr.ISISInterface) int {
		return cmp.Compare(x.Name, y.Name)
	})
	return s
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
	exportRTs := convertRTsToSliceOfStrings(vni.Spec.ExportRTs)
	importRTs := convertRTsToSliceOfStrings(vni.Spec.ImportRTs)

	if vni.Spec.HostSession == nil { // no neighbor, just the vni / vrf
		cfg := frr.L3VNIConfig{
			VNI:       vni.Spec.VNI,
			VRF:       vni.Spec.VRF,
			ASN:       underlayASN, // Since there is no session, the ASN is arbitrary
			RouterID:  routerID,
			ExportRTs: exportRTs,
			ImportRTs: importRTs,
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

	hostSideIPs, err := hostSessionToHostSideIPs(vni.Spec.HostSession, nodeIndex)
	if err != nil {
		return nil, err
	}

	configs := []frr.L3VNIConfig{}
	for _, af := range []ipfamily.Family{ipfamily.IPv4, ipfamily.IPv6} {
		ipnet, hasFamily := hostSideIPs[af]
		if !hasFamily {
			continue
		}
		toAdvertiseIPv4, toAdvertiseIPv6 := []string{}, []string{}
		if af == ipfamily.IPv4 {
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
			ExportRTs:       exportRTs,
			ImportRTs:       importRTs,
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

func l3vpnConfigsToFRR(l3VPNs []v1alpha1.L3VPN, routerID string, asn int64, nodeIndex int) ([]frr.L3VPNConfig, error) {
	vpnConfigs := []frr.L3VPNConfig{}
	for _, vpn := range l3VPNs {
		frrVNI, err := l3vpnToFRR(vpn, routerID, asn, nodeIndex)
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
	// importRTs cannot be auto-derived. Unfortunately, FRR does not support wildcard notation, e.g. *:200. And
	// using 0, e.g. 0:200, imports the route target verbatim.
	if len(vpn.Spec.ImportRTs) < 1 {
		return nil, errors.New("invalid configuration for importRTs, must provide at least one explicit import Route Target")
	}
	importRTs := convertRTsToSliceOfStrings(vpn.Spec.ImportRTs)

	exportRTs := defaultRTTargetsFor(underlayASN, vpn.Spec.RDAssignedNumber)
	if len(vpn.Spec.ExportRTs) > 0 {
		exportRTs = convertRTsToSliceOfStrings(vpn.Spec.ExportRTs)
	}

	if vpn.Spec.HostSession == nil { // no neighbor, just the vni / vrf
		conf := []frr.L3VPNConfig{
			{
				ASN:                underlayASN, // Since there is no session, the ASN is arbitrary
				VRF:                vpn.Spec.VRF,
				RouterID:           routerID,
				ExportRTs:          exportRTs,
				ImportRTs:          importRTs,
				RouteDistinguisher: routeDistinguisher(routerID, vpn.Spec.RDAssignedNumber),
			},
		}
		return conf, nil
	}

	hostASN, err := frr.NewPeerASN(vpn.Spec.HostSession.HostASN, vpn.Spec.HostSession.HostType)
	if err != nil {
		return nil, fmt.Errorf("could not parse HostSession, err: %w", err)
	}

	hostSideIPs, err := hostSessionToHostSideIPs(vpn.Spec.HostSession, nodeIndex)
	if err != nil {
		return nil, err
	}

	configs := []frr.L3VPNConfig{}
	for _, af := range []ipfamily.Family{ipfamily.IPv4, ipfamily.IPv6} {
		ipnet, hasFamily := hostSideIPs[af]
		if !hasFamily {
			continue
		}
		toAdvertiseIPv4, toAdvertiseIPv6 := []string{}, []string{}
		if af == ipfamily.IPv4 {
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

	return configs, nil
}

func routeDistinguisher(left string, right int32) string {
	return fmt.Sprintf("%s:%d", left, right)
}

func hostSessionToHostSideIPs(hostSession *v1alpha1.HostSession, nodeIndex int) (map[ipfamily.Family]net.IPNet, error) {
	veths, err := ipam.VethIPsFromPool(hostSession.LocalCIDR.IPv4, hostSession.LocalCIDR.IPv6, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get veths ips: %w", err)
	}

	hostSideIPs := map[ipfamily.Family]net.IPNet{}
	if ip := veths.Ipv4.HostSide.IP; ip != nil {
		hostSideIPs[ipfamily.IPv4] = net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
	}
	if ip := veths.Ipv6.HostSide.IP; ip != nil {
		hostSideIPs[ipfamily.IPv6] = net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
	}
	if len(hostSideIPs) == 0 {
		return nil, errors.New("no valid host side IP found")
	}
	return hostSideIPs, nil
}

// convertRTsToSliceOfStrings converts the provided routeTarget []v1alpha1.RouteTarget to slice of strings.
// convertRTsToSliceOfStrings does not validate the provided routeTargets:
// - for APItoFRR,  FilterValidL3VNIs -> validateL3VNI already did the validation
// - in validate_vni.go, validation is done separately.
func convertRTsToSliceOfStrings(routeTargets []v1alpha1.RouteTarget) []string {
	strTargets := make([]string, len(routeTargets))
	for i, rt := range routeTargets {
		strTargets[i] = string(rt)
	}
	return strTargets
}

func defaultRTTargetsFor(asn int64, rdAssignedNumber int32) []string {
	return []string{fmt.Sprintf("%d:%d", asn, rdAssignedNumber)}
}

func neighborToFRR(n v1alpha1.Neighbor,
	l2vnis []v1alpha1.L2VNI,
	l3vnis []v1alpha1.L3VNI,
	l3vpns []v1alpha1.L3VPN,
	l3passthroughs []v1alpha1.L3Passthrough,
	tunnelEndpoint *v1alpha1.TunnelEndpointConfig,
	segmentRouting *frr.UnderlaySegmentRouting,
) (*frr.NeighborConfig, error) {
	asn, err := frr.NewPeerASN(n.ASN, n.Type)
	if err != nil {
		return nil, fmt.Errorf("neighbor %s: could not parse ASN configuration, err: %w", neighborID(n), err)
	}

	neighName := neighborName(asn, neighborID(n))

	var nlps []networklayerprotocol.NLP
	if len(n.AddressFamilies) == 0 {
		nlps, err = defaultNLPsForNeighbor(n, l2vnis, l3vnis, l3vpns, l3passthroughs, tunnelEndpoint)
	} else {
		nlps, err = nlpsForNeighbor(n)
	}
	if err != nil {
		return nil, fmt.Errorf("neighbor %s: could not get network layer protocols, err: %w", neighName, err)
	}

	var updateSource string
	if neighborNeedsUpdateSource(segmentRouting, nlps) {
		updateSource = segmentRouting.SourceAddress
	}

	res := &frr.NeighborConfig{
		Name:                  neighName,
		ASN:                   asn,
		Addr:                  ptr.Deref(n.Address, ""),
		Interface:             ptr.Deref(n.Interface, ""),
		Port:                  n.Port,
		EBGPMultiHop:          ptr.Deref(n.EBGPMultiHop, false),
		Password:              ptr.Deref(n.Password, ""),
		UpdateSource:          updateSource,
		NetworkLayerProtocols: nlps,
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

// neighborNeedsUpdateSource determines if update source shall be set, or not. We set the update source only for
// SRv6 setups, meaning that SRv6 must be configured for the underlay and this neighbor must have an IPv4 or IPv6
// AFI with VPN SAFI in the networklayerprotocols.
func neighborNeedsUpdateSource(sr *frr.UnderlaySegmentRouting, nlps []networklayerprotocol.NLP) bool {
	if sr == nil {
		return false
	}
	if networklayerprotocol.HasNLP(nlps, networklayerprotocol.NLP{AFI: networklayerprotocol.IPv4, SAFI: networklayerprotocol.VPN}) {
		return true
	}
	if networklayerprotocol.HasNLP(nlps, networklayerprotocol.NLP{AFI: networklayerprotocol.IPv6, SAFI: networklayerprotocol.VPN}) {
		return true
	}
	return false
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

// setExtendedNexthopForNeighbor sets extended nexthop to true if the neighbor peers via an interface or if the neighbor
// peers via IPv6 and the exchanged network layer protocol is IPv4 unicast.
func setExtendedNexthopForNeighbor(res *frr.NeighborConfig) error {
	if res.Interface != "" {
		res.ExtendedNexthop = true
		return nil
	}

	neighborFamily, err := ipfamily.ForAddresses(res.Addr)
	if err != nil {
		return fmt.Errorf("failed to find ipfamily for %s, %w", res.Addr, err)
	}
	if neighborFamily == ipfamily.IPv4 {
		return nil
	}

	// Without `capability extended-nexthop`, IPv4 routes advertised via IPv6 peers will not be installed.
	// The same is true for IPv4 VPN routes advertised via IPv6 peers: their next hop would be set to the
	// IPv4 next-hop instead of the required IPv6 nexthop, and thus installing the route would fail.
	if networklayerprotocol.HasUnicastFamily(res.NetworkLayerProtocols, networklayerprotocol.IPv4) ||
		networklayerprotocol.HasVPNFamily(res.NetworkLayerProtocols, networklayerprotocol.IPv4) {
		res.ExtendedNexthop = true
	}
	return nil
}

// nlpsForNeighbor converts a neighbor's API neighbor IP families to internal representations.
func nlpsForNeighbor(n v1alpha1.Neighbor) ([]networklayerprotocol.NLP, error) {
	nlps := make([]networklayerprotocol.NLP, 0, len(n.AddressFamilies))
	for _, af := range n.AddressFamilies {
		switch af.Type {
		case "ipv4unicast":
			nlps = append(nlps, networklayerprotocol.NLP{
				AFI:  networklayerprotocol.IPv4,
				SAFI: networklayerprotocol.Unicast,
			})
		case "ipv6unicast":
			nlps = append(nlps, networklayerprotocol.NLP{
				AFI:  networklayerprotocol.IPv6,
				SAFI: networklayerprotocol.Unicast,
			})
		case "evpn":
			nlps = append(nlps, networklayerprotocol.NLP{
				AFI:  networklayerprotocol.L2VPN,
				SAFI: networklayerprotocol.EVPN,
			})
		case "ipv4vpn":
			nlps = append(nlps, networklayerprotocol.NLP{
				AFI:  networklayerprotocol.IPv4,
				SAFI: networklayerprotocol.VPN,
			})
		case "ipv6vpn":
			nlps = append(nlps, networklayerprotocol.NLP{
				AFI:  networklayerprotocol.IPv6,
				SAFI: networklayerprotocol.VPN,
			})
		default:
			return nil, fmt.Errorf("unsupported address family type %q", af.Type)
		}
	}
	return nlps, nil
}

// defaultNLPsForNeighbor parses a neighbor, l2vnis, l3vnis, l3vpns and l3passthroughs, tunnelEndpoint and chooses sane
// defaults.
// Defaults are chosen as follows:
// In any case, if tunnel endpoint CIDRs are configured, enabled the tunnel endpoint CIDR's families.
// For unnumbered neighbors:
// - ipv4unicast
// - ipv6unicast if passthrough is configured with IPv6 local CIDR
// - evpn if L2VNIs or L3VNIs are present.
// For IPv4 neighbors:
// - ipv4unicast
// - ipv6unicast if passthrough is configured with IPv6 local CIDR
// - evpn if L2VNIs or L3VNIs are present.
// For IPv6 neighbors:
// - ipv4unicast if L2VNIs or L3VNIs are present, or if passthrough is configured with IPv4 local CIDR
// - ipv6unicast
// - evpn if L2VNIs or L3VNIs are present
// - ipv4vpn if L3VPNs and SRv6 configuration are present.
// - ipv6vpn if L3VPNs and SRv6 configuration are present.
func defaultNLPsForNeighbor(n v1alpha1.Neighbor,
	l2vnis []v1alpha1.L2VNI, l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN, l3passthroughs []v1alpha1.L3Passthrough,
	tunnelEndpoint *v1alpha1.TunnelEndpointConfig,
) ([]networklayerprotocol.NLP, error) {
	addIPv4Unicast := false
	addIPv6Unicast := false
	addEVPN := false
	addIPv4VPN := false
	addIPv6VPN := false

	intf := ptr.Deref(n.Interface, "")
	addr := ptr.Deref(n.Address, "")
	address := net.ParseIP(addr)
	if intf == "" && address == nil {
		return nil, fmt.Errorf("either Interface or valid IP Address must be set to determine default, "+
			"interface: %s, address: %s", intf, addr)
	}
	isIPv6Neighbor := intf == "" && ipfamily.ForAddress(address) == ipfamily.IPv6

	if isIPv6Neighbor {
		addIPv6Unicast = true
	} else {
		addIPv4Unicast = true
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

	if tunnelEndpoint != nil {
		for _, cidr := range tunnelEndpoint.CIDRs {
			switch ipfamily.ForCIDRString(cidr) {
			case ipfamily.IPv4:
				addIPv4Unicast = true
			case ipfamily.IPv6:
				addIPv6Unicast = true
			}
		}
	}

	if isIPv6Neighbor && len(l3vpns) > 0 {
		addIPv4VPN = true
		addIPv6VPN = true
	}

	defaultNLPs := []networklayerprotocol.NLP{}
	if addIPv4Unicast {
		defaultNLPs = append(defaultNLPs, networklayerprotocol.NLP{
			AFI:  networklayerprotocol.IPv4,
			SAFI: networklayerprotocol.Unicast,
		})
	}
	if addIPv6Unicast {
		defaultNLPs = append(defaultNLPs, networklayerprotocol.NLP{
			AFI:  networklayerprotocol.IPv6,
			SAFI: networklayerprotocol.Unicast,
		})
	}
	if addEVPN {
		defaultNLPs = append(defaultNLPs, networklayerprotocol.NLP{
			AFI:  networklayerprotocol.L2VPN,
			SAFI: networklayerprotocol.EVPN,
		})
	}
	if addIPv4VPN {
		defaultNLPs = append(defaultNLPs, networklayerprotocol.NLP{
			AFI:  networklayerprotocol.IPv4,
			SAFI: networklayerprotocol.VPN,
		})
	}
	if addIPv6VPN {
		defaultNLPs = append(defaultNLPs, networklayerprotocol.NLP{
			AFI:  networklayerprotocol.IPv6,
			SAFI: networklayerprotocol.VPN,
		})
	}
	return defaultNLPs, nil
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
			vrf := l2vni.Name
			if l2vni.Spec.VRF != nil {
				vrf = *l2vni.Spec.VRF
			}
			res[vrf] = l2vni.Spec.L2GatewayIPs
		}
	}
	return res
}
