// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

// FilterOverlayResources returns valid overlays and all errors from field,
// RD, VNI, VRF, subnet, passthrough, and host-session validation.
func FilterOverlayResources(l3VNIs []v1alpha1.L3VNI, l3VPNs []v1alpha1.L3VPN,
	l2VNIs []v1alpha1.L2VNI, passthroughs []v1alpha1.L3Passthrough,
) ([]v1alpha1.L3VNI, []v1alpha1.L3VPN, []v1alpha1.L2VNI, []v1alpha1.L3Passthrough, error) {
	// NOTE: Check the current resource state and conflicts here. Do not reject
	// resources because a dependency is missing: resources may be created in
	// any order, and reconciliation will resolve dependencies as they appear.
	// Missing-dependency checks belong in reconciliation only.
	resourceErrors := make([]error, 0, 12)

	validL3VNIs, err := FilterValidL3VNIs(l3VNIs)
	resourceErrors = append(resourceErrors, err)

	validL3VPNs, err := FilterValidL3VPNs(l3VPNs)
	resourceErrors = append(resourceErrors, err)

	validL2VNIs, err := FilterValidL2VNIs(l2VNIs)
	resourceErrors = append(resourceErrors, err)

	validL3VPNs, err = FilterUniqueL3VPNRDNumbers(validL3VPNs)
	resourceErrors = append(resourceErrors, err)

	validL3VNIs, err = FilterUniqueL3VNIRDNumbers(validL3VNIs)
	resourceErrors = append(resourceErrors, err)

	validL2VNIs, err = FilterUniqueL2VNIRDNumbers(validL2VNIs)
	resourceErrors = append(resourceErrors, err)

	validL3VNIs, allocatedVNIs, err := FilterUniqueL3VNIs(validL3VNIs)
	resourceErrors = append(resourceErrors, err)

	validL2VNIs, err = FilterUniqueL2VNIs(validL2VNIs, allocatedVNIs)
	resourceErrors = append(resourceErrors, err)

	validL3VNIs, vrfToVNI, err := FilterUniqueVRFsForL3VNIs(validL3VNIs)
	resourceErrors = append(resourceErrors, err)

	validL3VPNs, err = FilterUniqueVRFsForL3VPNs(validL3VPNs, vrfToVNI)
	resourceErrors = append(resourceErrors, err)

	validPassthroughs, err := FilterValidPassthroughs(passthroughs)
	resourceErrors = append(resourceErrors, err)

	// Cross resource validation should go below here

	validL3VNIs, validL3VPNs, validL2VNIs, err = FilterValidVRFSubnets(validL3VNIs, validL3VPNs, validL2VNIs)
	resourceErrors = append(resourceErrors, err)

	if err := ValidateHostSessions(validL3VNIs, validPassthroughs); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to validate host sessions: %w", err)
	}

	return validL3VNIs, validL3VPNs, validL2VNIs, validPassthroughs, errors.Join(resourceErrors...)
}
