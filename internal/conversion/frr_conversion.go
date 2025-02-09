// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"net"
	"time"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/ipam"
	"github.com/openperouter/openperouter/internal/ipfamily"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type FRRConversionError struct {
	msg string
}

func (e FRRConversionError) Error() string {
	return e.msg
}

func APItoFRR(nodeIndex int, underlays []v1alpha1.Underlay, vnis []v1alpha1.VNI, logLevel string) (frr.Config, error) {
	if len(underlays) > 1 {
		return frr.Config{}, FRRConversionError{msg: "can't have more than one underlay"}
	}
	if len(underlays) == 0 {
		return frr.Config{}, FRRConversionError{msg: "no underlays defined"}
	}
	if len(vnis) == 0 {
		return frr.Config{}, FRRConversionError{msg: "no vnis defined"}
	}

	underlay := underlays[0]
	vtepIP, err := ipam.VTEPIp(underlay.Spec.VTEPCIDR, nodeIndex)
	if err != nil {
		return frr.Config{}, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIntex %d", underlay.Spec.VTEPCIDR, nodeIndex)
	}

	underlayNeighbors := []frr.NeighborConfig{}
	for _, n := range underlay.Spec.Neighbors {
		frrNeigh, err := neighborToFRR(n)
		if err != nil {
			return frr.Config{}, fmt.Errorf("failed to translate underlay neighbor %s to frr, err: %w", neighborName(n), err)
		}
		underlayNeighbors = append(underlayNeighbors, *frrNeigh)
	}
	underlayConfig := frr.UnderlayConfig{
		MyASN:     underlay.Spec.ASN,
		VTEP:      vtepIP.IP.String(),
		Neighbors: underlayNeighbors,
	}
	vniConfigs := []frr.VNIConfig{}
	for _, vni := range vnis {
		frrVNI, err := vniToFRR(vni, nodeIndex)
		if err != nil {
			return frr.Config{}, fmt.Errorf("failed to translate vni to frr: %w, vni %v", err, vni)
		}
		vniConfigs = append(vniConfigs, frrVNI)
	}

	return frr.Config{
		Underlay: underlayConfig,
		VNIs:     vniConfigs,
		Loglevel: logLevel,
	}, nil
}

func vniToFRR(vni v1alpha1.VNI, nodeIndex int) (frr.VNIConfig, error) {
	veths, err := ipam.VethIPs(vni.Spec.LocalCIDR, nodeIndex)
	if err != nil {
		return frr.VNIConfig{}, fmt.Errorf("failed to get veths ips for vni %s: %w", vni.Name, err)
	}

	vniNeighbor := &frr.NeighborConfig{
		Addr: veths.HostSide.IP.String(),
	}
	vniNeighbor.ASN = vni.Spec.ASN
	if vni.Spec.HostASN != nil {
		vniNeighbor.ASN = *vni.Spec.HostASN
	}

	// since the traffic is normally masqueraded, we advertise the node ip so
	// the return traffic is able to come to the node
	hostSideIPToAdvertise := net.IPNet{
		IP:   veths.HostSide.IP,
		Mask: net.CIDRMask(32, 32), // TODO ipv6
	}

	res := frr.VNIConfig{
		ASN:           vni.Spec.ASN,
		VNI:           int(vni.Spec.VNI),
		VRF:           vni.VRFName(),
		LocalNeighbor: vniNeighbor,
		ToAdvertise:   []string{hostSideIPToAdvertise.String()},
	}
	return res, nil
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
		BFDProfile:   n.BFDProfile,
	}
	res.HoldTime, res.KeepaliveTime, err = parseTimers(n.HoldTime, n.KeepaliveTime)
	if err != nil {
		return nil, fmt.Errorf("invalid timers for neighbor %s, err: %w", neighborName(n), err)
	}

	if n.ConnectTime != nil {
		res.ConnectTime = ptr.To(uint64(n.ConnectTime.Duration / time.Second))
	}

	/* TODO passwords
	res.Password, err = passwordForNeighbor(n, passwordSecrets)
	if err != nil {
		return nil, err
	} */
	return res, nil
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

	htSeconds := uint64(holdTime / time.Second)
	kaSeconds := uint64(keepaliveTime / time.Second)

	return &htSeconds, &kaSeconds, nil
}
