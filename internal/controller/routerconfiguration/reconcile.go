// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/frr"
)

func Reconcile(ctx context.Context, apiConfig conversion.APIConfigData, nodeIndex int, logLevel, frrConfigPath, targetNamespace string, updater frr.ConfigUpdater, hostConfigurator HostConfigurator) error {
	normalizeConfig(&apiConfig)
	if err := conversion.ValidateUnderlays(apiConfig.Underlays); err != nil {
		return fmt.Errorf("failed to validate underlays: %w", err)
	}

	if conversion.HasMissingSRv6ForL3VPNs(apiConfig.Underlays, apiConfig.L3VPNs) {
		return conversion.MissingSRv6ForL3VPNErrors(apiConfig.Underlays, apiConfig.L3VPNs, nil)
	}

	var resourceErrors []error
	var err error

	var validL3VNIs []v1alpha1.L3VNI
	validL3VNIs, err = conversion.FilterValidL3VNIs(apiConfig.L3VNIs)
	resourceErrors = append(resourceErrors, err)

	var validL3VPNs []v1alpha1.L3VPN
	validL3VPNs, err = conversion.FilterValidL3VPNs(apiConfig.L3VPNs)
	resourceErrors = append(resourceErrors, err)

	if err := conversion.DetectMutuallyExclusiveOverlays(validL3VNIs, validL3VPNs); err != nil {
		return errors.Join(append(resourceErrors, err)...)
	}

	var validL2VNIs []v1alpha1.L2VNI
	validL2VNIs, err = conversion.FilterValidL2VNIs(apiConfig.L2VNIs)
	resourceErrors = append(resourceErrors, err)

	var vnis map[int32]string
	validL3VNIs, vnis, err = conversion.FilterUniqueL3VNIs(validL3VNIs)
	resourceErrors = append(resourceErrors, err)

	var rdAssignedNumbers map[int32]string
	validL3VPNs, rdAssignedNumbers, err = conversion.FilterUniqueL3VPNs(validL3VPNs)
	resourceErrors = append(resourceErrors, err)
	// TODO: This is safe today, but may cause issues when we change to per-VRF mutual exclusivity
	// for L3VNI and L3VPN.
	maps.Copy(vnis, rdAssignedNumbers)

	validL2VNIs, err = conversion.FilterUniqueL2VNIs(validL2VNIs, vnis)
	resourceErrors = append(resourceErrors, err)

	var vrfs sets.Set[string]
	validL3VNIs, vrfs, err = conversion.FilterUniqueVRFsForL3VNIs(validL3VNIs)
	resourceErrors = append(resourceErrors, err)

	var vrfsForL3VPN sets.Set[string]
	validL3VPNs, vrfsForL3VPN, err = conversion.FilterUniqueVRFsForL3VPNs(validL3VPNs)
	resourceErrors = append(resourceErrors, err)
	vrfs = vrfs.Union(vrfsForL3VPN)

	validL3VNIs, validL3VPNs, validL2VNIs, err = conversion.FilterValidVRFSubnets(validL3VNIs, validL3VPNs, validL2VNIs)
	resourceErrors = append(resourceErrors, err)

	validL2VNIs, err = filterL2VNIsWithoutVRF(validL2VNIs, vrfs)
	resourceErrors = append(resourceErrors, err)

	var validPassthrough []v1alpha1.L3Passthrough
	validPassthrough, err = conversion.FilterValidPassthroughs(apiConfig.L3Passthrough)
	resourceErrors = append(resourceErrors, err)

	if err := conversion.ValidateHostSessions(validL3VNIs, validPassthrough); err != nil {
		return fmt.Errorf("failed to validate host sessions: %w", err)
	}

	config := conversion.APIConfigData{
		Underlays:     apiConfig.Underlays,
		L3VNIs:        validL3VNIs,
		L3VPNs:        validL3VPNs,
		L2VNIs:        validL2VNIs,
		L3Passthrough: validPassthrough,
		RawFRRConfigs: apiConfig.RawFRRConfigs,
	}

	err = hostConfigurator(ctx, interfacesConfiguration{
		targetNamespace: targetNamespace,
		APIConfigData:   config,
		nodeIndex:       nodeIndex,
	})
	if openpeerrors.IsNonResourceError(err) {
		return err
	}
	resourceErrors = append(resourceErrors, err)

	if err = configureFRR(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		APIConfigData: config,
		nodeIndex:     nodeIndex,
		logLevel:      logLevel,
	}); err != nil {
		return err
	}

	return errors.Join(resourceErrors...)
}

// filterL2VNIsWithoutVRF must be called after all L3VNI / L3VPN filtering is complete.
func filterL2VNIsWithoutVRF(l2Vnis []v1alpha1.L2VNI, existingVRFs sets.Set[string]) ([]v1alpha1.L2VNI, error) {
	var valid []v1alpha1.L2VNI
	var resourceErrors []error
	for _, l2 := range l2Vnis {
		if l2.Spec.VRF != nil && *l2.Spec.VRF != "" && !existingVRFs.Has(*l2.Spec.VRF) {
			resourceErrors = append(resourceErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind:    openpeerrors.KindL2VNI,
					Name:    l2.Name,
					Reason:  v1alpha1.FailedResourceReasonDependencyFailed,
					Message: fmt.Sprintf("no valid L3 resource for VRF %q", *l2.Spec.VRF),
				},
			})
			continue
		}
		valid = append(valid, l2)
	}
	return valid, errors.Join(resourceErrors...)
}

// normalizeConfig sorts resources by namespace/name so validation order is deterministic.
func normalizeConfig(config *conversion.APIConfigData) {
	slices.SortFunc(config.L3VNIs, func(a, b v1alpha1.L3VNI) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})

	slices.SortFunc(config.L3VPNs, func(a, b v1alpha1.L3VPN) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})

	slices.SortFunc(config.L2VNIs, func(a, b v1alpha1.L2VNI) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})

	slices.SortFunc(config.L3Passthrough, func(a, b v1alpha1.L3Passthrough) int {
		return cmp.Compare(objectKey(&a), objectKey(&b))
	})
}

func objectKey(o client.Object) string {
	return client.ObjectKeyFromObject(o).String()
}
