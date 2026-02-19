// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	defaultRouterIDCidr = "10.0.0.0/24"
)

type FRREmptyConfigError string

func (e FRREmptyConfigError) Error() string {
	return string(e)
}

func APItoFRR(config ApiConfigData, nodeIndex int, logLevel string) (frr.Config, error) {
	if len(config.Underlays) > 1 {
		return frr.Config{}, errors.New("multiple underlays defined")
	}
	if len(config.Underlays) == 0 {
		return frr.Config{}, FRREmptyConfigError("no underlays provided")
	}
	if len(config.L3Passthrough) > 1 {
		return frr.Config{}, errors.New("multiple passthrough defined, can have only one")
	}

	underlay := config.Underlays[0]

	underlayNeighbors := []frr.NeighborConfig{}
	bfdProfiles := []frr.BFDProfile{}
	for _, n := range underlay.Spec.Neighbors {
		frrNeigh, err := neighborToFRR(n)
		if err != nil {
			return frr.Config{}, fmt.Errorf("failed to translate underlay neighbor %s to frr, err: %w", neighborName(n), err)
		}

		bfdProfile := bfdProfileForNeighbor(n)
		underlayNeighbors = append(underlayNeighbors, *frrNeigh)
		if bfdProfile != nil {
			bfdProfiles = append(bfdProfiles, *bfdProfile)
		}
	}

	routerID, err := routerIDFromUnderlay(underlay, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to get routerID: %w", err)
	}

	underlayConfig := frr.UnderlayConfig{
		MyASN:     underlay.Spec.ASN,
		RouterID:  routerID,
		Neighbors: underlayNeighbors,
	}

	var passthroughConfig *frr.PassthroughConfig
	if len(config.L3Passthrough) > 0 {
		passthrough, err := passthroughToFRR(config.L3Passthrough[0], nodeIndex)
		if err != nil {
			return frr.Config{}, fmt.Errorf("failed to translate passthrough to frr: %w", err)
		}
		passthroughConfig = passthrough
	}

	if len(config.L3VNIs) > 0 && underlay.Spec.EVPN == nil {
		return frr.Config{}, fmt.Errorf("EVPN configuration is required when L3 VNIs are defined")
	}
	if underlay.Spec.EVPN == nil {
		return frr.Config{
			Underlay:    underlayConfig,
			Passthrough: passthroughConfig,
			BFDProfiles: bfdProfiles,
			Loglevel:    logLevel,
			VNIs:        []frr.L3VNIConfig{},
		}, nil
	}

	underlayConfig.EVPN = &frr.UnderlayEvpn{}
	if underlay.Spec.EVPN.VTEPCIDR != "" {
		vtepIP, err := ipam.VTEPIp(underlay.Spec.EVPN.VTEPCIDR, nodeIndex)
		if err != nil {
			return frr.Config{}, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIndex %d: %w", underlay.Spec.EVPN.VTEPCIDR, nodeIndex, err)
		}
		underlayConfig.EVPN.VTEP = vtepIP.String()
	}

	vniConfigs := []frr.L3VNIConfig{}
	for _, vni := range config.L3VNIs {
		frrVNI, err := l3vniToFRR(vni, routerID, underlay.Spec.ASN, nodeIndex)
		if err != nil {
			return frr.Config{}, fmt.Errorf("failed to translate vni to frr: %w, vni %v", err, vni)
		}
		vniConfigs = append(vniConfigs, frrVNI...)
	}

	return frr.Config{
		Underlay:    underlayConfig,
		VNIs:        vniConfigs,
		Passthrough: passthroughConfig,
		BFDProfiles: bfdProfiles,
		Loglevel:    logLevel,
	}, nil
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

	if vethIPs.Ipv4.HostSide.IP != nil {
		res.LocalNeighborV4 = &frr.NeighborConfig{
			ASN:  passthrough.Spec.HostSession.HostASN,
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
			ASN:  passthrough.Spec.HostSession.HostASN,
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

func l3vniToFRR(vni v1alpha1.L3VNI, routerID string, underlayASN uint32, nodeIndex int) ([]frr.L3VNIConfig, error) {
	if vni.Spec.HostSession == nil { // no neighbor, just the vni / vrf
		return []frr.L3VNIConfig{
			{
				VNI:      int(vni.Spec.VNI),
				VRF:      vni.Spec.VRF,
				ASN:      underlayASN, // Since there is no session, the ASN is arbitrary
				RouterID: routerID,
			},
		}, nil
	}

	veths, err := ipam.VethIPsFromPool(vni.Spec.HostSession.LocalCIDR.IPv4, vni.Spec.HostSession.LocalCIDR.IPv6, nodeIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get veths ips for vni %s: %w", vni.Name, err)
	}

	var configs []frr.L3VNIConfig

	// Create IPv4 neighbor if IPv4 IP is available
	if veths.Ipv4.HostSide.IP != nil {
		config := createVNIConfig(vni, veths.Ipv4.HostSide.IP, net.CIDRMask(32, 32), routerID)
		configs = append(configs, config)
	}

	// Create IPv6 neighbor if IPv6 IP is available
	if veths.Ipv6.HostSide.IP != nil {
		config := createVNIConfig(vni, veths.Ipv6.HostSide.IP, net.CIDRMask(128, 128), routerID)
		configs = append(configs, config)
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no valid host side IP found for vni %s", vni.Name)
	}

	return configs, nil
}

// createVNIConfig creates a VNI configuration for a specific IP family
func createVNIConfig(vni v1alpha1.L3VNI, hostIP net.IP, mask net.IPMask, routerID string) frr.L3VNIConfig {
	vniNeighbor := &frr.NeighborConfig{
		Addr: hostIP.String(),
	}
	vniNeighbor.ASN = vni.Spec.HostSession.ASN
	if vni.Spec.HostSession.HostASN != 0 {
		vniNeighbor.ASN = vni.Spec.HostSession.HostASN
	}

	ipnet := net.IPNet{
		IP:   hostIP,
		Mask: mask,
	}

	config := frr.L3VNIConfig{
		ASN:           vni.Spec.HostSession.ASN,
		VNI:           int(vni.Spec.VNI),
		VRF:           vni.Spec.VRF,
		RouterID:      routerID,
		LocalNeighbor: vniNeighbor,
	}

	ipFamily := ipfamily.ForAddress(hostIP)
	if ipFamily == ipfamily.IPv4 {
		config.ToAdvertiseIPv4 = []string{ipnet.String()}
		config.ToAdvertiseIPv6 = []string{}
		return config
	}

	// Else ipv6

	config.ToAdvertiseIPv4 = []string{}
	config.ToAdvertiseIPv6 = []string{ipnet.String()}
	return config
}

func neighborToFRR(n v1alpha1.Neighbor) (*frr.NeighborConfig, error) {
	neighborFamily, err := ipfamily.ForAddresses(n.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to find ipfamily for %s, %w", n.Address, err)
	}

	if n.ASN == 0 {
		return nil, fmt.Errorf("neighbor %s does not have ASN", n.Address)
	}

	res := &frr.NeighborConfig{
		Name:         neighborName(n),
		ASN:          n.ASN,
		Addr:         n.Address,
		Port:         n.Port,
		IPFamily:     neighborFamily,
		EBGPMultiHop: n.EBGPMultiHop,
	}
	res.HoldTime, res.KeepaliveTime, err = parseTimers(n.HoldTime, n.KeepaliveTime)
	if err != nil {
		return nil, fmt.Errorf("invalid timers for neighbor %s, err: %w", neighborName(n), err)
	}

	if n.ConnectTime != nil {
		connectSecond, err := durationToUint64(n.ConnectTime.Duration / time.Second)
		if err != nil {
			return nil, fmt.Errorf("invalid connecttime %v: %w", n.ConnectTime.Duration, err)
		}
		res.ConnectTime = ptr.To(connectSecond)
	}

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

func neighborName(n v1alpha1.Neighbor) string {
	return fmt.Sprintf("%d@%s", n.ASN, n.Address)
}

func parseTimers(ht, ka *metav1.Duration) (*uint64, *uint64, error) {
	if ht == nil && ka != nil || ht != nil && ka == nil {
		return nil, nil, fmt.Errorf("one of KeepaliveTime/HoldTime specified, both must be set or none")
	}

	if ht == nil && ka == nil {
		return nil, nil, nil
	}

	holdTime := ht.Duration
	keepaliveTime := ka.Duration

	rounded := time.Duration(int(ht.Seconds())) * time.Second
	if rounded != 0 && rounded < 3*time.Second {
		return nil, nil, fmt.Errorf("invalid hold time %q: must be 0 or >=3s", ht)
	}

	if keepaliveTime > holdTime {
		return nil, nil, fmt.Errorf("invalid keepaliveTime %q, must be lower than holdTime %q", ka, ht)
	}

	htSeconds, err := durationToUint64(holdTime / time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid hold time %v: %w", holdTime, err)
	}
	kaSeconds, err := durationToUint64(keepaliveTime / time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid keepalive time %v: %w", holdTime, err)
	}

	return &htSeconds, &kaSeconds, nil
}

func durationToUint64(value time.Duration) (uint64, error) {
	if value < 0 {
		return 0, fmt.Errorf("cannot convert negative value to uint64: %d", value)
	}
	return uint64(value), nil // #nosec G115
}

func routerIDFromUnderlay(underlay v1alpha1.Underlay, nodeIndex int) (string, error) {
	routerIDCidr := underlay.Spec.RouterIDCIDR
	if underlay.Spec.RouterIDCIDR == "" {
		routerIDCidr = defaultRouterIDCidr
		slog.Info("empty routerid cidr, using the default one", "underlay", underlay.Name, "default cidr", defaultRouterIDCidr)
	}
	routerID, err := ipam.RouterID(routerIDCidr, nodeIndex)
	if err != nil {
		return "", fmt.Errorf("failed to get router id, cidr %s, nodeIndex %d: %w", underlay.Spec.RouterIDCIDR, nodeIndex, err)
	}
	return routerID, nil
}
