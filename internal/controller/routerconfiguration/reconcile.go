// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"

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

	if err := conversion.ValidateL3VNIs(apiConfig.L3VNIs); err != nil {
		return result, fmt.Errorf("failed to validate l3vnis: %w", err)
	}

	if err := conversion.ValidateL2VNIs(apiConfig.L2VNIs); err != nil {
		return result, fmt.Errorf("failed to validate l2vnis: %w", err)
	}

	if err := conversion.ValidateVRFs(apiConfig.L2VNIs, apiConfig.L3VNIs); err != nil {
		return result, fmt.Errorf("failed to validate VRFs: %w", err)
	}

	if err := conversion.ValidatePassthroughs(apiConfig.L3Passthrough); err != nil {
		return result, fmt.Errorf("failed to validate l3passthrough: %w", err)
	}

	if err := conversion.ValidateHostSessions(apiConfig.L3VNIs, apiConfig.L3Passthrough); err != nil {
		return result, fmt.Errorf("failed to validate host sessions: %w", err)
	}

	if err := configureFRR(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		APIConfigData: apiConfig,
		nodeIndex:     nodeIndex,
		logLevel:      logLevel,
	}); err != nil {
		return result, fmt.Errorf("failed to reload frr config: %w", err)
	}

	if err := configureInterfaces(ctx, interfacesConfiguration{
		targetNamespace:    targetNamespace,
		APIConfigData:      apiConfig,
		nodeIndex:          nodeIndex,
		underlayFromMultus: underlayFromMultus,
	}); err != nil {
		return result, fmt.Errorf("failed to configure the host: %w", err)
	}

	return result, nil
}

func underlayName(apiConfig conversion.APIConfigData) string {
	if len(apiConfig.Underlays) > 0 {
		return apiConfig.Underlays[0].Name
	}
	return ""
}
