// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
)

type frrConfigData struct {
	configFile string
	updater    frr.ConfigUpdater
	conversion.ApiConfigData
	nodeIndex int
	logLevel  string
}

func configureFRR(ctx context.Context, data frrConfigData) error {
	slog.DebugContext(ctx, "reloading FRR config", "config", data)
	frrConfig, err := conversion.APItoFRR(data.ApiConfigData, data.nodeIndex, data.logLevel)
	emptyConfig := conversion.FRREmptyConfigError("")
	if errors.As(err, &emptyConfig) {
		slog.InfoContext(ctx, "reloading FRR config", "empty config", data, "event", "cleaning the frr configuration")
		frrConfig = frr.Config{}
	}
	if err != nil && !errors.As(err, &emptyConfig) {
		return fmt.Errorf("failed to generate the frr configuration: %w", err)
	}

	err = frr.ApplyConfig(ctx, &frrConfig, data.updater)
	if err != nil {
		return fmt.Errorf("failed to update the frr configuration: %w", err)
	}
	return nil
}
