// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/filter"
)

type hostSessionInfo struct {
	v1alpha1.HostSession
	name string
}

func ValidateHostSessionsForNodes(nodes []corev1.Node, l3VNIs []v1alpha1.L3VNI, l3Passthrough []v1alpha1.L3Passthrough) error {
	for _, node := range nodes {
		filteredL3VNIs, err := filter.FilterL3VNIsForNode(&node, l3VNIs)
		if err != nil {
			return fmt.Errorf("failed to filter L3 VNIs for node %q: %w", node.Name, err)
		}
		filteredL3Passthroughs, err := filter.FilterL3PassthroughsForNode(&node, l3Passthrough)
		if err != nil {
			return fmt.Errorf("failed to filter L3 Passthrough for node %q: %w", node.Name, err)
		}
		if err := ValidateHostSessions(filteredL3VNIs, filteredL3Passthroughs); err != nil {
			return fmt.Errorf("failed to validate host sessions for node %q: %w", node.Name, err)
		}
	}
	return nil
}

func ValidateHostSessions(l3VNIs []v1alpha1.L3VNI, l3Passthrough []v1alpha1.L3Passthrough) error {
	hostSessions := []hostSessionInfo{}
	for _, vni := range l3VNIs {
		if vni.Spec.HostSession == nil {
			continue
		}
		hostSessions = append(hostSessions, hostSessionInfo{HostSession: *vni.Spec.HostSession, name: "l3vni " + vni.Name})
	}
	for _, passthrough := range l3Passthrough {
		hostSessions = append(hostSessions, hostSessionInfo{HostSession: passthrough.Spec.HostSession, name: "l3passthrough " + passthrough.Name})
	}

	existingCIDRsV4 := map[string]string{}
	existingCIDRsV6 := map[string]string{}
	for _, s := range hostSessions {
		if s.HostASN == s.ASN {
			return fmt.Errorf("%s local ASN %d must be different from remote ASN %d", s.name, s.HostASN, s.ASN)
		}
		if s.LocalCIDR.IPv4 != "" {
			if err := validateCIDR(s, s.LocalCIDR.IPv4, existingCIDRsV4); err != nil {
				return err
			}
			existingCIDRsV4[s.LocalCIDR.IPv4] = s.name
		}
		if s.LocalCIDR.IPv6 != "" {
			if err := validateCIDR(s, s.LocalCIDR.IPv6, existingCIDRsV6); err != nil {
				return err
			}
			existingCIDRsV6[s.LocalCIDR.IPv6] = s.name
		}
		if s.LocalCIDR.IPv4 == "" && s.LocalCIDR.IPv6 == "" {
			return fmt.Errorf("at least one local CIDR (IPv4 or IPv6) must be provided for vni %s", s.name)
		}
	}
	return nil
}

// validateCIDR validates a single CIDR and checks for overlaps with existing CIDRs
func validateCIDR(session hostSessionInfo, cidr string, existingCIDRs map[string]string) error {
	if err := isValidCIDR(cidr); err != nil {
		return fmt.Errorf("invalid local CIDR %s for vni %s: %w", cidr, session.name, err)
	}
	for existing, existingVNI := range existingCIDRs {
		overlap, err := cidrsOverlap(existing, cidr)
		if err != nil {
			return err
		}
		if overlap {
			return fmt.Errorf("overlapping cidrs %s - %s for vnis %s - %s", existing, cidr, existingVNI, session.name)
		}
	}
	return nil
}
