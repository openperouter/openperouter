// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
)

func Reconcile(ctx context.Context, apiConfig conversion.APIConfigData, underlayFromMultus bool, nodeIndex int, logLevel, frrConfigPath, targetNamespace string, updater frr.ConfigUpdater) (ReconcileResult, error) {
	var result ReconcileResult

	if err := conversion.ValidateUnderlays(apiConfig.Underlays); err != nil {
		result.AddFailure("Underlay", underlayName(apiConfig), v1alpha1.ValidationFailed, err.Error())
		return result, nil
	}

	usedVNIs := map[uint32]string{}
	quarantinedVRFs := sets.New[string]()

	validL3VNIs, validVRFs := validateL3VNIsWithQuarantine(apiConfig.L3VNIs, usedVNIs, quarantinedVRFs, &result)
	validL2VNIs := validateL2VNIsWithQuarantine(apiConfig.L2VNIs, usedVNIs, quarantinedVRFs, validVRFs, &result)
	validPassthrough := validatePassthroughsWithQuarantine(apiConfig.L3Passthrough, &result)

	if err := conversion.ValidateVRFs(validL2VNIs, validL3VNIs); err != nil {
		return result, fmt.Errorf("failed to validate VRFs: %w", err)
	}

	if err := conversion.ValidateHostSessions(validL3VNIs, validPassthrough); err != nil {
		return result, fmt.Errorf("failed to validate host sessions: %w", err)
	}

	validConfig := conversion.APIConfigData{
		Underlays:     apiConfig.Underlays,
		L3VNIs:        validL3VNIs,
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

	ifConfig := interfacesConfiguration{
		targetNamespace:    targetNamespace,
		APIConfigData:      validConfig,
		nodeIndex:          nodeIndex,
		underlayFromMultus: underlayFromMultus,
	}
	hostConfig, err := setupHostPrerequisites(ctx, ifConfig)
	if err != nil {
		return result, fmt.Errorf("failed to configure the host: %w", err)
	}

	if len(validConfig.Underlays) > 0 {
		if err := provisionOverlays(ctx, ifConfig, hostConfig); err != nil {
			return result, fmt.Errorf("failed to provision overlays: %w", err)
		}
	}

	return result, nil
}

func underlayName(apiConfig conversion.APIConfigData) string {
	if len(apiConfig.Underlays) > 0 {
		return apiConfig.Underlays[0].Name
	}
	return ""
}

func validateL3VNIsWithQuarantine(l3VNIs []v1alpha1.L3VNI, usedVNIs map[uint32]string, quarantinedVRFs sets.Set[string], result *ReconcileResult) ([]v1alpha1.L3VNI, map[string]string) {
	var valid []v1alpha1.L3VNI
	usedVRFs := map[string]string{}

	for _, l3 := range l3VNIs {
		if err := conversion.ValidateL3VNI(l3); err != nil {
			result.AddFailure("L3VNI", l3.Name, v1alpha1.ValidationFailed, err.Error())
			quarantinedVRFs.Insert(l3.Spec.VRF)
			continue
		}
		if existing, ok := usedVRFs[l3.Spec.VRF]; ok {
			result.AddFailure("L3VNI", l3.Name, v1alpha1.ValidationFailed,
				fmt.Sprintf("duplicate VRF %s, already used by L3VNI %s", l3.Spec.VRF, existing))
			continue
		}
		if existing, ok := usedVNIs[l3.Spec.VNI]; ok {
			result.AddFailure("L3VNI", l3.Name, v1alpha1.ValidationFailed,
				fmt.Sprintf("duplicate VNI %d, already used by %s", l3.Spec.VNI, existing))
			quarantinedVRFs.Insert(l3.Spec.VRF)
			continue
		}
		usedVRFs[l3.Spec.VRF] = l3.Name
		usedVNIs[l3.Spec.VNI] = "L3VNI/" + l3.Name
		valid = append(valid, l3)
	}
	return valid, usedVRFs
}

func validateL2VNIsWithQuarantine(l2VNIs []v1alpha1.L2VNI, usedVNIs map[uint32]string, quarantinedVRFs sets.Set[string], validVRFs map[string]string, result *ReconcileResult) []v1alpha1.L2VNI {
	var valid []v1alpha1.L2VNI
	for _, l2 := range l2VNIs {
		if l2.Spec.VRF != nil && *l2.Spec.VRF != "" {
			if quarantinedVRFs.Has(*l2.Spec.VRF) {
				result.AddFailure("L2VNI", l2.Name, v1alpha1.DependencyFailed,
					fmt.Sprintf("VRF %s is quarantined", *l2.Spec.VRF))
				continue
			}
			if _, ok := validVRFs[*l2.Spec.VRF]; !ok {
				result.AddFailure("L2VNI", l2.Name, v1alpha1.DependencyFailed,
					fmt.Sprintf("no L3VNI found for VRF %s", *l2.Spec.VRF))
				continue
			}
		}
		if err := conversion.ValidateL2VNI(l2); err != nil {
			result.AddFailure("L2VNI", l2.Name, v1alpha1.ValidationFailed, err.Error())
			continue
		}
		if existing, ok := usedVNIs[l2.Spec.VNI]; ok {
			result.AddFailure("L2VNI", l2.Name, v1alpha1.ValidationFailed,
				fmt.Sprintf("duplicate VNI %d, already used by %s", l2.Spec.VNI, existing))
			continue
		}
		usedVNIs[l2.Spec.VNI] = "L2VNI/" + l2.Name
		valid = append(valid, l2)
	}
	return valid
}

func validatePassthroughsWithQuarantine(passthroughs []v1alpha1.L3Passthrough, result *ReconcileResult) []v1alpha1.L3Passthrough {
	if err := conversion.ValidatePassthroughs(passthroughs); err != nil {
		for _, pt := range passthroughs {
			result.AddFailure("L3Passthrough", pt.Name, v1alpha1.ValidationFailed, err.Error())
		}
		return nil
	}
	return passthroughs
}
