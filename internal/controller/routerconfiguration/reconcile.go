// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	openpeerrors "github.com/openperouter/openperouter/internal/errors"
	"github.com/openperouter/openperouter/internal/frr"
)

func Reconcile(ctx context.Context, apiConfig conversion.APIConfigData, nodeIndex int, logLevel, frrConfigPath, targetNamespace string, updater frr.ConfigUpdater, hostConfigurator HostConfigurator) error {
	normalizeConfig(&apiConfig)
	if err := conversion.ValidateUnderlays(apiConfig.Underlays); err != nil {
		return err
	}

	var allErrors []error
	var err error

	var validL3VNIs []v1alpha1.L3VNI
	validL3VNIs, err = conversion.FilterValidL3VNIs(apiConfig.L3VNIs)
	allErrors = append(allErrors, err)

	var validL2VNIs []v1alpha1.L2VNI
	validL2VNIs, err = conversion.FilterValidL2VNIs(apiConfig.L2VNIs)
	allErrors = append(allErrors, err)

	validL3VNIs, validL2VNIs, err = conversion.FilterUniqueVNIs(validL3VNIs, validL2VNIs)
	allErrors = append(allErrors, err)

	validL3VNIs, err = conversion.FilterUniqueVRFs(validL3VNIs)
	allErrors = append(allErrors, err)

	validL3VNIs, validL2VNIs, err = conversion.FilterValidVRFSubnets(validL3VNIs, validL2VNIs)
	allErrors = append(allErrors, err)

	validL2VNIs, err = filterL2VNIsWithoutL3VNI(validL2VNIs, validL3VNIs)
	allErrors = append(allErrors, err)

	var validPassthrough []v1alpha1.L3Passthrough
	validPassthrough, err = conversion.FilterValidPassthroughs(apiConfig.L3Passthrough)
	allErrors = append(allErrors, err)

	if err := conversion.ValidateHostSessions(validL3VNIs, validPassthrough); err != nil {
		return fmt.Errorf("failed to validate host sessions: %w", err)
	}

	config := conversion.APIConfigData{
		Underlays:     apiConfig.Underlays,
		L3VNIs:        validL3VNIs,
		L2VNIs:        validL2VNIs,
		L3Passthrough: validPassthrough,
		RawFRRConfigs: apiConfig.RawFRRConfigs,
	}

	if err := hostConfigurator(ctx, interfacesConfiguration{
		targetNamespace: targetNamespace,
		APIConfigData:   config,
		nodeIndex:       nodeIndex,
	}); err != nil {
		return err
	}

	if err := configureFRR(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		APIConfigData: config,
		nodeIndex:     nodeIndex,
		logLevel:      logLevel,
	}); err != nil {
		return fmt.Errorf("failed to reload frr config: %w", err)
	}

	return errors.Join(allErrors...)
}

// filterL2VNIsWithoutL3VNI must be called after all L3VNI filtering is complete.
func filterL2VNIsWithoutL3VNI(l2Vnis []v1alpha1.L2VNI, l3Vnis []v1alpha1.L3VNI) ([]v1alpha1.L2VNI, error) {
	vrfs := sets.New[string]()
	for _, l3 := range l3Vnis {
		vrfs.Insert(l3.Spec.VRF)
	}
	var valid []v1alpha1.L2VNI
	var allErrors []error
	for _, l2 := range l2Vnis {
		if l2.Spec.VRF != nil && *l2.Spec.VRF != "" && !vrfs.Has(*l2.Spec.VRF) {
			allErrors = append(allErrors, &openpeerrors.ResourceError{
				Obj: v1alpha1.FailedResource{
					Kind:    openpeerrors.KindL2VNI,
					Name:    l2.Name,
					Reason:  v1alpha1.FailedResourceReasonDependencyFailed,
					Message: fmt.Sprintf("no valid L3VNI for L3 domain %q", *l2.Spec.VRF),
				},
			})
			continue
		}
		valid = append(valid, l2)
	}
	return valid, errors.Join(allErrors...)
}

func normalizeConfig(config *conversion.APIConfigData) {
	slices.SortFunc(config.L3VNIs, func(a, b v1alpha1.L3VNI) int { return cmp.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name) })

	slices.SortFunc(config.L2VNIs, func(a, b v1alpha1.L2VNI) int { return cmp.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name) })

	slices.SortFunc(config.L3Passthrough, func(a, b v1alpha1.L3Passthrough) int {
		return cmp.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})
}
