// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"math"
	"slices"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	corev1 "k8s.io/api/core/v1"
)

// ValidateRoutingIsolation checks dependencies before FRR or the datapath is changed.
func ValidateRoutingIsolation(config APIConfigData) error {
	var source string
	if len(config.Underlays) == 1 && config.Underlays[0].Spec.SRV6 != nil {
		if configured := config.Underlays[0].Spec.SRV6.EncapsulationSource; configured != nil {
			source = *configured
		}
	}
	var table int64
	legacy := false
	for _, vpn := range config.L3VPNs {
		isolation := vpn.Spec.RoutingIsolation
		if isolation == nil {
			legacy = true
			continue
		}
		if isolation.UnderlayTable < 1 || isolation.UnderlayTable > math.MaxUint32 {
			return fmt.Errorf("underlayTable must be in 1..4294967295")
		}
		if slices.Contains(vpn.Spec.Features, v1alpha1.UDT4UDT6) && isolation.UnderlayTable != 254 {
			return fmt.Errorf("L3VPN %s: uDT4uDT6 preserves underlay iif: only main underlay table is supported; use per-VRF SID for a dedicated table", vpn.Name)
		}
		params := hostnetwork.SRv6Isolation{Source: source, UnderlayTable: uint32(isolation.UnderlayTable),
			RemoteLocatorPrefixes: isolation.RemoteLocatorPrefixes}
		if err := params.Validate(); err != nil {
			return fmt.Errorf("L3VPN %s routingIsolation: %w", vpn.Name, err)
		}
		// Locally originated encapsulation loses the original output VRF on relookup.
		if table != 0 && table != isolation.UnderlayTable {
			return fmt.Errorf("isolated SRv6 VRFs in one namespace must share underlayTable")
		}
		table = isolation.UnderlayTable
		if err := validateMixedVRFASN(config, vpn); err != nil {
			return err
		}
	}
	if legacy && table != 0 && table != 254 {
		return fmt.Errorf("legacy and dedicated-table SRv6 VPNs cannot share a namespace: local-output lookup cannot distinguish VPNs")
	}
	return nil
}

// ValidateRoutingIsolationForNodes checks transport dependencies after node selection.
func ValidateRoutingIsolationForNodes(nodes []corev1.Node, underlays []v1alpha1.Underlay,
	vpns []v1alpha1.L3VPN, vnis []v1alpha1.L3VNI) error {
	for _, node := range nodes {
		u, err := filter.UnderlaysForNode(&node, underlays)
		if err != nil {
			return err
		}
		v, err := filter.L3VPNsForNode(&node, vpns)
		if err != nil {
			return err
		}
		e, err := filter.L3VNIsForNode(&node, vnis)
		if err != nil {
			return err
		}
		if err := ValidateRoutingIsolation(APIConfigData{Underlays: u, L3VPNs: v, L3VNIs: e}); err != nil {
			return fmt.Errorf("node %s: %w", node.Name, err)
		}
	}
	return nil
}

func validateMixedVRFASN(config APIConfigData, vpn v1alpha1.L3VPN) error {
	for _, vni := range config.L3VNIs {
		if vni.Spec.VRF != vpn.Spec.VRF {
			continue
		}
		vniASN, vpnASN := int64(0), int64(0)
		if len(config.Underlays) == 1 {
			vniASN, vpnASN = config.Underlays[0].Spec.ASN, config.Underlays[0].Spec.ASN
		}
		if vni.Spec.HostSession != nil {
			vniASN = vni.Spec.HostSession.ASN
		}
		if vpn.Spec.HostSession != nil {
			vpnASN = vpn.Spec.HostSession.ASN
		}
		if vniASN != vpnASN {
			return fmt.Errorf("mixed VRF %s must use the same hostSession ASN for both transports", vpn.Spec.VRF)
		}
	}
	return nil
}

// ValidateEncapsulationSourceUsage checks the final configuration, after resource
// filtering. Admission allows the underlay and its VPNs to be created separately.
func ValidateEncapsulationSourceUsage(config APIConfigData) error {
	if len(config.Underlays) != 1 || config.Underlays[0].Spec.SRV6 == nil {
		return nil
	}
	if config.Underlays[0].Spec.SRV6.EncapsulationSource == nil {
		return nil
	}
	for _, vpn := range config.L3VPNs {
		if vpn.Spec.RoutingIsolation != nil {
			return nil
		}
	}
	return fmt.Errorf("encapsulationSource requires at least one L3VPN with routingIsolation")
}
