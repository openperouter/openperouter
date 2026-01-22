// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"

	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/status"
)

func Reconcile(ctx context.Context, apiConfig conversion.ApiConfigData, frrConfigPath, targetNamespace string, updater frr.ConfigUpdater, statusReporter status.StatusReporter) error {

	if err := conversion.ValidateUnderlays(apiConfig.Underlays, statusReporter); err != nil {
		return fmt.Errorf("failed to validate underlays: %w", err)
	}

	if err := conversion.ValidateL3VNIs(apiConfig.L3VNIs, statusReporter); err != nil {
		return fmt.Errorf("failed to validate l3vnis: %w", err)
	}

	if err := conversion.ValidateL2VNIs(apiConfig.L2VNIs, statusReporter); err != nil {
		return fmt.Errorf("failed to validate l2vnis: %w", err)
	}

	if err := conversion.ValidatePassthroughs(apiConfig.L3Passthrough, statusReporter); err != nil {
		return fmt.Errorf("failed to validate l3passthrough: %w", err)
	}

	if err := conversion.ValidateHostSessions(apiConfig.L3VNIs, apiConfig.L3Passthrough, statusReporter); err != nil {
		return fmt.Errorf("failed to validate host sessions: %w", err)
	}

	if err := configureFRR(ctx, frrConfigData{
		configFile:    frrConfigPath,
		updater:       updater,
		ApiConfigData: apiConfig,
	}); err != nil {
		return fmt.Errorf("failed to reload frr config: %w", err)
	}

	if err := configureInterfaces(ctx, interfacesConfiguration{
		StatusReporter:  statusReporter,
		targetNamespace: targetNamespace,
		ApiConfigData:   apiConfig,
	}); err != nil {
		return fmt.Errorf("failed to configure the host: %w", err)
	}

	return nil
}
