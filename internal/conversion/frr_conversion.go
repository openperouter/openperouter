// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"log/slog"
	"net"
	"sort"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"
	"k8s.io/utils/ptr"
)

const (
	defaultRouterIDCidr = "10.0.0.0/24"
)

type FRREmptyConfigError string

func (e FRREmptyConfigError) Error() string {
	return string(e)
}

// APItoFRR converts API custom resources into an frr.Config.
func APItoFRR(config APIConfigData, nodeIndex int, logLevel string) (frr.Config, error) {
	rawSnippets := rawConfigSnippets(config.RawFRRConfigs)
	// If we have raw config, we apply it regardless of the rest of the
	// configuration. Otherwise, the FRR conversion rejects an empty underlay.
	if len(rawSnippets) > 0 && len(config.Underlays) == 0 {
		slog.Info("no underlay provided, applying raw configuration only")
		return frr.Config{
			Loglevel:  logLevel,
			RawConfig: rawSnippets,
		}, nil
	}

	if err := validateAPIConfigData(config); err != nil {
		return frr.Config{}, err
	}

	underlay := config.Underlays[0]

	routerID, err := routerIDFromUnderlay(underlay, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to get routerID: %w", err)
	}

	underlayNeighbors, err := apiToFRRNeighborConfig(underlay)
	if err != nil {
		return frr.Config{}, err
	}

	underlayConfigEVPN, err := apiToFRREVPN(underlay, nodeIndex)
	if err != nil {
		return frr.Config{}, err
	}

	underlayConfig := frr.UnderlayConfig{
		MyASN:     underlay.Spec.ASN,
		RouterID:  routerID,
		Neighbors: underlayNeighbors,
		EVPN:      underlayConfigEVPN,
	}

	passthroughConfig, err := apiToFRRPassthroughConfig(config.L3Passthrough, nodeIndex)
	if err != nil {
		return frr.Config{}, err
	}

	vniConfigs, err := apiToFRRL3VNIConfig(underlay, config.L3VNIs, routerID, nodeIndex)
	if err != nil {
		return frr.Config{}, err
	}

	return frr.Config{
		Underlay:    underlayConfig,
		VNIs:        vniConfigs,
		Passthrough: passthroughConfig,
		BFDProfiles: apiToFRRBFDProfile(underlay),
		Loglevel:    logLevel,
		RawConfig:   rawSnippets,
	}, nil
}

func apiToFRRNeighborConfig(underlay v1alpha1.Underlay) ([]frr.NeighborConfig, error) {
	underlayNeighbors := []frr.NeighborConfig{}
	for _, n := range underlay.Spec.Neighbors {
		frrNeigh, err := neighborToFRR(n)
		if err != nil {
			return nil, fmt.Errorf("failed to translate underlay neighbor to frr, err: %w", err)
		}
		underlayNeighbors = append(underlayNeighbors, *frrNeigh)
	}
	return underlayNeighbors, nil
}

func apiToFRREVPN(underlay v1alpha1.Underlay, nodeIndex int) (*frr.UnderlayEvpn, error) {
	if underlay.Spec.EVPN == nil {
		return nil, nil
	}
	underlayConfigEVPN := &frr.UnderlayEvpn{}
	if vtepCIDR := ptr.Deref(underlay.Spec.EVPN.VTEPCIDR, ""); vtepCIDR != "" {
		vtepIP, err := ipam.VTEPIp(vtepCIDR, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w",
				vtepCIDR, nodeIndex, err)
		}
		underlayConfigEVPN.VTEP = vtepIP.String()
	}
	return underlayConfigEVPN, nil
}

func apiToFRRPassthroughConfig(l3Passthrough []v1alpha1.L3Passthrough, nodeIndex int) (*frr.PassthroughConfig, error) {
	if len(l3Passthrough) == 0 {
		return nil, nil
	}
	passthroughConfig, err := passthroughToFRR(l3Passthrough[0], nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to translate passthrough to frr: %w", err)
	}
	return passthroughConfig, nil
}

func apiToFRRL3VNIConfig(underlay v1alpha1.Underlay, l3VNIs []v1alpha1.L3VNI, routerID string,
	nodeIndex int) ([]frr.L3VNIConfig, error) {
	vniConfigs := []frr.L3VNIConfig{}
	for _, vni := range l3VNIs {
		frrVNI, err := l3vniToFRR(vni, routerID, underlay.Spec.ASN, nodeIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to translate vni to frr: %w, vni %v", err, vni)
		}
		vniConfigs = append(vniConfigs, frrVNI...)
	}
	return vniConfigs, nil
}

func apiToFRRBFDProfile(underlay v1alpha1.Underlay) []frr.BFDProfile {
	bfdProfiles := []frr.BFDProfile{}
	for _, n := range underlay.Spec.Neighbors {
		bfdProfile := bfdProfileForNeighbor(n)
		if bfdProfile != nil {
			bfdProfiles = append(bfdProfiles, *bfdProfile)
		}
	}
	return bfdProfiles
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

func passthroughToFRR(passthrough v1alpha1.L3Passthrough, nodeIndex int) (*frr.PassthroughConfig, error) {
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
func l3vniToFRR(vni v1alpha1.L3VNI, routerID string, underlayASN int64, nodeIndex int) ([]frr.L3VNIConfig, error) {
	if vni.Spec.HostSession == nil { // no neighbor, just the vni / vrf
		return []frr.L3VNIConfig{
			{
				VNI:       vni.Spec.VNI,
				VRF:       vni.Spec.VRF,
				ASN:       underlayASN, // Since there is no session, the ASN is arbitrary
				RouterID:  routerID,
				ExportRTs: vni.Spec.ExportRTs,
				ImportRTs: vni.Spec.ImportRTs,
			},
		}, nil
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
			ASN:             vni.Spec.HostSession.ASN,
			VNI:             vni.Spec.VNI,
			VRF:             vni.Spec.VRF,
			RouterID:        routerID,
			LocalNeighbor:   &frr.NeighborConfig{Addr: ipnet.IP.String(), ASN: hostASN},
			ExportRTs:       vni.Spec.ExportRTs,
			ImportRTs:       vni.Spec.ImportRTs,
			ToAdvertiseIPv4: toAdvertiseIPv4,
			ToAdvertiseIPv6: toAdvertiseIPv6,
		})
	}
	return configs, nil
}

func neighborToFRR(n v1alpha1.Neighbor) (*frr.NeighborConfig, error) {
	asn, err := frr.NewPeerASN(n.ASN, n.Type)
	if err != nil {
		return nil, fmt.Errorf("neighbor at address %s: could not parse ASN configuration, err: %w", n.Address, err)
	}

	neighName := neighborName(asn, n.Address)

	neighborFamily, err := ipfamily.ForAddresses(n.Address)
	if err != nil {
		return nil, fmt.Errorf("neighbor %s: failed to find IP family for address %s, %w", neighName, n.Address, err)
	}

	res := &frr.NeighborConfig{
		Name:         neighName,
		ASN:          asn,
		Addr:         n.Address,
		Port:         n.Port,
		IPFamily:     neighborFamily,
		EBGPMultiHop: ptr.Deref(n.EBGPMultiHop, false),
		Password:     ptr.Deref(n.Password, ""),
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

func bfdProfileNameForNeighbor(n v1alpha1.Neighbor) string {
	return fmt.Sprintf("neighbor-%s", n.Address)
}

func neighborName(asn frr.PeerASN, address string) string {
	return fmt.Sprintf("%s@%s", asn, address)
}

func routerIDFromUnderlay(underlay v1alpha1.Underlay, nodeIndex int) (string, error) {
	routerIDCidr := ptr.Deref(underlay.Spec.RouterIDCIDR, "")
	if routerIDCidr == "" {
		slog.Info("empty routerid cidr, using the default one", "underlay", underlay.Name, "default cidr", defaultRouterIDCidr)
		routerIDCidr = defaultRouterIDCidr
	}
	routerID, err := ipam.RouterID(routerIDCidr, nodeIndex)
	if err != nil {
		return "", fmt.Errorf("failed to get router id, cidr %s, nodeIndex %d: %w", routerIDCidr, nodeIndex, err)
	}
	return routerID, nil
}
