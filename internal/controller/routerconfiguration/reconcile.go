// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
)

func Reconcile(ctx context.Context, apiConfig conversion.APIConfigData, nodeIndex int, logLevel, frrConfigPath, targetNamespace string, updater frr.ConfigUpdater, hostConfigurator HostConfigurator) error {
	normalizeConfig(&apiConfig)
	if err := conversion.ValidateUnderlays(apiConfig.Underlays); err != nil {
		return err
	}

	if err := conversion.ValidateL3VNIs(apiConfig.L3VNIs); err != nil {
		return fmt.Errorf("failed to validate l3vnis: %w", err)
	}

	if err := conversion.ValidateL2VNIs(apiConfig.L2VNIs); err != nil {
		return fmt.Errorf("failed to validate l2vnis: %w", err)
	}

	if err := conversion.ValidateVRFs(apiConfig.L2VNIs, apiConfig.L3VNIs); err != nil {
		return fmt.Errorf("failed to validate VRFs: %w", err)
	}

	if err := conversion.ValidatePassthroughs(apiConfig.L3Passthrough); err != nil {
		return fmt.Errorf("failed to validate l3passthrough: %w", err)
	}

	if err := conversion.ValidateHostSessions(apiConfig.L3VNIs, apiConfig.L3Passthrough); err != nil {
		return fmt.Errorf("failed to validate host sessions: %w", err)
	}

	if err := hostConfigurator(ctx, interfacesConfiguration{
		targetNamespace: targetNamespace,
		APIConfigData:   apiConfig,
		nodeIndex:       nodeIndex,
	}); err != nil {
		return err
	}

	if err := configureFRR(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		APIConfigData: apiConfig,
		nodeIndex:     nodeIndex,
		logLevel:      logLevel,
	}); err != nil {
		return fmt.Errorf("failed to reload frr config: %w", err)
	}

	return nil
}

func normalizeConfig(config *conversion.APIConfigData) {
	slices.SortFunc(config.L3VNIs, func(a, b v1alpha1.L3VNI) int { return cmp.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name) })

	slices.SortFunc(config.L2VNIs, func(a, b v1alpha1.L2VNI) int { return cmp.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name) })

	slices.SortFunc(config.L3Passthrough, func(a, b v1alpha1.L3Passthrough) int {
		return cmp.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})
}
