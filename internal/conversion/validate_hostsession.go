// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	v1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
)

type hostSessionInfo struct {
	v1alpha1.HostSession
	name string
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
