// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
)

func Reconcile(ctx context.Context, apiConfig conversion.APIConfigData, underlayFromMultus bool, nodeIndex int, logLevel, frrConfigPath, targetNamespace string, updater frr.ConfigUpdater, hostConfigurator HostConfigurator) (ReconcileResult, error) {
	var result ReconcileResult

	if err := conversion.ValidateUnderlays(apiConfig.Underlays); err != nil {
		result.AddFailure(KindUnderlay, underlayName(apiConfig), v1alpha1.ValidationFailed, err.Error())
		return result, nil
	}

	usedVNIs := map[int32]string{}

	l3Result, l3Failures := validateL3VNIsWithQuarantine(apiConfig.L3VNIs, usedVNIs)
	result.Merge(l3Failures)

	validL2VNIs, l2Failures := validateL2VNIsWithQuarantine(apiConfig.L2VNIs, usedVNIs, l3Result.ValidVRFs)
	result.Merge(l2Failures)

	validPassthrough, ptFailures := validatePassthroughsWithQuarantine(apiConfig.L3Passthrough)
	result.Merge(ptFailures)

	if err := conversion.ValidateVRFs(validL2VNIs, l3Result.ValidL3VNIs); err != nil {
		return result, fmt.Errorf("failed to validate VRFs: %w", err)
	}

	if err := conversion.ValidateHostSessions(l3Result.ValidL3VNIs, validPassthrough); err != nil {
		return result, fmt.Errorf("failed to validate host sessions: %w", err)
	}

	validConfig := conversion.APIConfigData{
		Underlays:     apiConfig.Underlays,
		L3VNIs:        l3Result.ValidL3VNIs,
		L2VNIs:        validL2VNIs,
		L3Passthrough: validPassthrough,
		RawFRRConfigs: apiConfig.RawFRRConfigs,
	}

	if err := configureFRR(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		APIConfigData: validConfig,
		nodeIndex:     nodeIndex,
		logLevel:      logLevel,
	}); err != nil {
		return result, fmt.Errorf("failed to reload frr config: %w", err)
	}

	hostResult, err := hostConfigurator(ctx, interfacesConfiguration{
		targetNamespace:    targetNamespace,
		APIConfigData:      validConfig,
		nodeIndex:          nodeIndex,
		underlayFromMultus: underlayFromMultus,
	})
	result.Merge(hostResult)
	if err != nil {
		return result, err
	}

	return result, nil
}

func underlayName(apiConfig conversion.APIConfigData) string {
	if len(apiConfig.Underlays) > 0 {
		return apiConfig.Underlays[0].Name
	}
	return ""
}

type l3ValidationResult struct {
	ValidL3VNIs []v1alpha1.L3VNI
	ValidVRFs   map[string]string
}

func validateL3VNIsWithQuarantine(l3VNIs []v1alpha1.L3VNI, usedVNIs map[int32]string) (l3ValidationResult, ReconcileResult) {
	var result ReconcileResult
	var valid []v1alpha1.L3VNI
	usedVRFs := map[string]string{}

	for _, l3 := range l3VNIs {
		if err := conversion.ValidateL3VNI(l3); err != nil {
			result.AddFailure(KindL3VNI, l3.Name, v1alpha1.ValidationFailed, err.Error())
			continue
		}
		if existing, ok := usedVRFs[l3.Spec.VRF]; ok {
			result.AddFailure(KindL3VNI, l3.Name, v1alpha1.ValidationFailed,
				fmt.Sprintf("duplicate VRF %s, already used by L3VNI %s", l3.Spec.VRF, existing))
			continue
		}
		if existing, ok := usedVNIs[l3.Spec.VNI]; ok {
			result.AddFailure(KindL3VNI, l3.Name, v1alpha1.ValidationFailed,
				fmt.Sprintf("duplicate VNI %d, already used by %s", l3.Spec.VNI, existing))
			continue
		}
		usedVRFs[l3.Spec.VRF] = l3.Name
		usedVNIs[l3.Spec.VNI] = KindL3VNI + "/" + l3.Name
		valid = append(valid, l3)
	}
	return l3ValidationResult{ValidL3VNIs: valid, ValidVRFs: usedVRFs}, result
}

func validateL2VNIsWithQuarantine(l2VNIs []v1alpha1.L2VNI, usedVNIs map[int32]string, validVRFs map[string]string) ([]v1alpha1.L2VNI, ReconcileResult) {
	var result ReconcileResult
	var valid []v1alpha1.L2VNI
	for _, l2 := range l2VNIs {
		if l2.Spec.VRF != nil && *l2.Spec.VRF != "" {
			if _, ok := validVRFs[*l2.Spec.VRF]; !ok {
				result.AddFailure(KindL2VNI, l2.Name, v1alpha1.DependencyFailed,
					fmt.Sprintf("no valid L3VNI for VRF %s", *l2.Spec.VRF))
				continue
			}
		}
		if err := conversion.ValidateL2VNI(l2); err != nil {
			result.AddFailure(KindL2VNI, l2.Name, v1alpha1.ValidationFailed, err.Error())
			continue
		}
		if existing, ok := usedVNIs[l2.Spec.VNI]; ok {
			result.AddFailure(KindL2VNI, l2.Name, v1alpha1.ValidationFailed,
				fmt.Sprintf("duplicate VNI %d, already used by %s", l2.Spec.VNI, existing))
			continue
		}
		usedVNIs[l2.Spec.VNI] = KindL2VNI + "/" + l2.Name
		valid = append(valid, l2)
	}
	return valid, result
}

func validatePassthroughsWithQuarantine(passthroughs []v1alpha1.L3Passthrough) ([]v1alpha1.L3Passthrough, ReconcileResult) {
	var result ReconcileResult
	if err := conversion.ValidatePassthroughs(passthroughs); err != nil {
		for _, pt := range passthroughs {
			result.AddFailure(KindL3Passthrough, pt.Name, v1alpha1.ValidationFailed, err.Error())
		}
		return nil, result
	}
	return passthroughs, result
}
