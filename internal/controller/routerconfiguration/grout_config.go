// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/grout"
)

type groutConfiguration struct {
	targetNamespace    string
	underlayFromMultus bool
	nodeIndex          int
	conversion.ApiConfigData
	groutSocketPath string
}

func configureGroutDataPath(ctx context.Context, config groutConfiguration) error {
	groutClient := grout.NewClient(config.groutSocketPath)

	hasAlreadyUnderlay, err := grout.HasUnderlayInterface(ctx, groutClient)
	if err != nil {
		return fmt.Errorf("failed to check if target namespace %s has underlay: %w", config.targetNamespace, err)
	}
	if hasAlreadyUnderlay && len(config.Underlays) == 0 {
		return UnderlayRemovedError{}
	}

	if len(config.Underlays) == 0 {
		return nil // nothing to do
	}

	slog.InfoContext(ctx, "configure interface start", "namespace", config.targetNamespace)
	defer slog.InfoContext(ctx, "configure interface end", "namespace", config.targetNamespace)
	apiConfig := conversion.ApiConfigData{
		Underlays:     config.Underlays,
		L3VNIs:        config.L3VNIs,
		L2VNIs:        config.L2VNIs,
		L3Passthrough: config.L3Passthrough,
	}
	hostConfig, err := conversion.APItoHostConfig(config.nodeIndex, config.targetNamespace, config.underlayFromMultus, apiConfig)
	if err != nil {
		return fmt.Errorf("failed to convert config to host configuration: %w", err)
	}

	slog.InfoContext(ctx, "setting up underlay")
	if err := grout.SetupUnderlay(ctx, groutClient, hostConfig.Underlay); err != nil {
		return fmt.Errorf("failed to setup underlay: %w", err)
	}

	slog.InfoContext(ctx, "setting up passthrough")
	if hostConfig.L3Passthrough != nil {
		if err := grout.SetupPassthrough(ctx, groutClient, *hostConfig.L3Passthrough); err != nil {
			return fmt.Errorf("failed to setup passthrough: %w", err)
		}
	}

	if len(apiConfig.L3Passthrough) == 0 {
		if err := grout.RemovePassthrough(ctx, groutClient, config.targetNamespace); err != nil {
			return fmt.Errorf("failed to remove passthrough: %w", err)
		}
	}
	return nil
}
