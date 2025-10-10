// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	v1alpha1 "github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/status"
)

type hostSessionInfo struct {
	v1alpha1.HostSession
	name         string
	resourceKind status.ResourceKind
	resourceName string
}

func ValidateHostSessions(l3VNIs []v1alpha1.L3VNI, l3Passthrough []v1alpha1.L3Passthrough, statusReporter status.StatusReporter) error {
	hostSessions := []hostSessionInfo{}
	for _, vni := range l3VNIs {
		if vni.Spec.HostSession == nil {
			continue
		}
		hostSessions = append(hostSessions, hostSessionInfo{
			HostSession:  *vni.Spec.HostSession,
			name:         "l3vni " + vni.Name,
			resourceKind: status.L3VNIKind,
			resourceName: vni.Name,
		})
	}
	for _, passthrough := range l3Passthrough {
		hostSessions = append(hostSessions, hostSessionInfo{
			HostSession:  passthrough.Spec.HostSession,
			name:         "l3passthrough " + passthrough.Name,
			resourceKind: status.L3PassthroughKind,
			resourceName: passthrough.Name,
		})
	}

	existingCIDRsV4 := map[string]string{}
	existingCIDRsV6 := map[string]string{}
	for _, s := range hostSessions {
		if s.HostASN == s.ASN {
			err := fmt.Errorf("%s local ASN %d must be different from remote ASN %d", s.name, s.HostASN, s.ASN)
			statusReporter.ReportResourceFailure(s.resourceKind, s.resourceName, err)
			return err
		}
		if s.LocalCIDR.IPv4 != "" {
			if err := validateCIDR(s, s.LocalCIDR.IPv4, existingCIDRsV4, statusReporter); err != nil {
				return err
			}
			existingCIDRsV4[s.LocalCIDR.IPv4] = s.name
		}
		if s.LocalCIDR.IPv6 != "" {
			if err := validateCIDR(s, s.LocalCIDR.IPv6, existingCIDRsV6, statusReporter); err != nil {
				return err
			}
			existingCIDRsV6[s.LocalCIDR.IPv6] = s.name
		}
		if s.LocalCIDR.IPv4 == "" && s.LocalCIDR.IPv6 == "" {
			err := fmt.Errorf("at least one local CIDR (IPv4 or IPv6) must be provided for vni %s", s.name)
			statusReporter.ReportResourceFailure(s.resourceKind, s.resourceName, err)
			return err
		}
	}
	return nil
}

// validateCIDR validates a single CIDR and checks for overlaps with existing CIDRs
func validateCIDR(session hostSessionInfo, cidr string, existingCIDRs map[string]string, statusReporter status.StatusReporter) error {
	if err := isValidCIDR(cidr); err != nil {
		validationErr := fmt.Errorf("invalid local CIDR %s for vni %s: %w", cidr, session.name, err)
		statusReporter.ReportResourceFailure(session.resourceKind, session.resourceName, validationErr)
		return validationErr
	}
	for existing, existingVNI := range existingCIDRs {
		overlap, err := cidrsOverlap(existing, cidr)
		if err != nil {
			statusReporter.ReportResourceFailure(session.resourceKind, session.resourceName, err)
			return err
		}
		if overlap {
			validationErr := fmt.Errorf("overlapping cidrs %s - %s for vnis %s - %s", existing, cidr, existingVNI, session.name)
			statusReporter.ReportResourceFailure(session.resourceKind, session.resourceName, validationErr)
			return validationErr
		}
	}
	return nil
}
