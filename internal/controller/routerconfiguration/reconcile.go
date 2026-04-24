// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"

	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
)

func Reconcile(ctx context.Context, apiConfig conversion.ApiConfigData, underlayFromMultus bool, groutEnabled bool, groutSocketPath string, nodeIndex int, logLevel, frrConfigPath, targetNamespace string, updater frr.ConfigUpdater) error {
	if err := conversion.ValidateGrout(groutEnabled, apiConfig); err != nil {
		return fmt.Errorf("failed grout validation: %w", err)
	}

	if err := conversion.ValidateUnderlays(apiConfig.Underlays); err != nil {
		return fmt.Errorf("failed to validate underlays: %w", err)
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

	if err := configureFRR(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		ApiConfigData: apiConfig,
		nodeIndex:     nodeIndex,
		logLevel:      logLevel,
	}); err != nil {
		return fmt.Errorf("failed to reload frr config: %w", err)
	}

	if groutEnabled {
		err := configureGroutDataPath(ctx, groutConfiguration{
			targetNamespace:    targetNamespace,
			underlayFromMultus: underlayFromMultus,
			nodeIndex:          nodeIndex,
			ApiConfigData:      apiConfig,
			groutSocketPath:    groutSocketPath,
		})
		if err != nil {
			return fmt.Errorf("failed to configure grout datapath: %w", err)
		}
	} else {
		err := configureInterfaces(ctx, interfacesConfiguration{
			targetNamespace:    targetNamespace,
			ApiConfigData:      apiConfig,
			nodeIndex:          nodeIndex,
			underlayFromMultus: underlayFromMultus,
		})
		if err != nil {
			return fmt.Errorf("failed to configure the host: %w", err)
		}
	}

	return nil
}
