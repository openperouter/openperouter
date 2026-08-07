// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"errors"
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"k8s.io/apimachinery/pkg/types"
)

// FilterUniqueVRFs checks VRF uniqueness among L3VNIs and L3VPNs and returns the valid
// L3VNIs and L3VPNs alongside per-resource errors for duplicates. L3VNIs are processed
// first, meaning that in case of VRF duplicates between L3VNIs and L3VPNs, the corresponding
// L3VPNs will report an error.
func FilterUniqueVRFs(l3vnis []v1alpha1.L3VNI, l3vpns []v1alpha1.L3VPN) ([]v1alpha1.L3VNI, []v1alpha1.L3VPN, error) {
	reason := v1alpha1.FailedResourceReasonValidationFailed
	var allErrors []error

	vrfToVNI := map[string]types.NamespacedName{}
	var validL3VNIs []v1alpha1.L3VNI
	for _, l3vni := range l3vnis {
		namespaceName := types.NamespacedName{Namespace: l3vni.Namespace, Name: l3vni.Name}
		if existing, duplicateFound := vrfToVNI[l3vni.Spec.VRF]; duplicateFound {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VNI", Name: l3vni.Name, Reason: reason,
					Message: fmt.Sprintf("more than one L3VNI detected in VRF %q: %q already exists", l3vni.Spec.VRF, existing),
				},
			})
			continue
		}
		vrfToVNI[l3vni.Spec.VRF] = namespaceName
		validL3VNIs = append(validL3VNIs, l3vni)
	}

	vrfToVPN := map[string]types.NamespacedName{}
	var validL3VPNs []v1alpha1.L3VPN
	for _, l3vpn := range l3vpns {
		namespaceName := types.NamespacedName{Namespace: l3vpn.Namespace, Name: l3vpn.Name}
		if existing, duplicateFound := vrfToVPN[l3vpn.Spec.VRF]; duplicateFound {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VPN", Name: l3vpn.Name, Reason: reason,
					Message: fmt.Sprintf("more than one L3VPN detected in VRF %q: %q already exists", l3vpn.Spec.VRF, existing),
				},
			})
			continue
		}
		if existing, duplicateFound := vrfToVNI[l3vpn.Spec.VRF]; duplicateFound {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind: "L3VPN", Name: l3vpn.Name, Reason: reason,
					Message: fmt.Sprintf("conflict with L3VNI detected in VRF %q: %q already exists", l3vpn.Spec.VRF, existing),
				},
			})
			continue
		}
		vrfToVPN[l3vpn.Spec.VRF] = namespaceName
		validL3VPNs = append(validL3VPNs, l3vpn)
	}

	return validL3VNIs, validL3VPNs, errors.Join(allErrors...)
}
