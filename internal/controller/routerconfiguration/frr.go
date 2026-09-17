// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
)

type frrConfigData struct {
	configFile string
	updater    frr.ConfigUpdater
	conversion.APIConfigData
	nodeIndex int
	logLevel  string
}

type frrConfiguratorType func(ctx context.Context, data frrConfigData) error

func configureFRR(ctx context.Context, data frrConfigData) error {
	slog.DebugContext(ctx, "reloading FRR config", "config", redactFrrConfigData(data))
	frrConfig, err := conversion.APItoFRR(data.APIConfigData, data.nodeIndex, data.logLevel)
	emptyConfig := conversion.NoUnderlaysError("")
	if errors.As(err, &emptyConfig) {
		slog.InfoContext(ctx, "reloading FRR config", "event", "cleaning the frr configuration")
		frrConfig = frr.Config{}
	}
	if err != nil && !errors.As(err, &emptyConfig) {
		return fmt.Errorf("failed to generate the frr configuration: %w", err)
	}

	previousConfig, prevErr := os.ReadFile(data.configFile)
	err = frr.ApplyConfig(ctx, &frrConfig, data.updater)
	if err != nil {
		return fmt.Errorf("failed to update the frr configuration: %w", err)
	}
	newConfig, newErr := os.ReadFile(data.configFile)
	if newErr != nil {
		slog.WarnContext(ctx, "failed to read FRR config after update", "error", newErr)
		return nil
	}
	if prevErr != nil {
		slog.InfoContext(ctx, "no previous FRR config to compare", "error", prevErr)
	}
	if prevErr != nil || string(previousConfig) != string(newConfig) {
		slog.InfoContext(ctx, "FRR configuration updated", "config", frr.RedactPasswords(string(newConfig)))
	}
	return nil
}

// redactFrrConfigData returns a copy of data safe for logging, with resolved
// BGP passwords and any passwords in raw FRR snippets redacted.
func redactFrrConfigData(data frrConfigData) frrConfigData {
	data.APIConfigData = conversion.RedactAPIConfigData(data.APIConfigData)
	return data
}
