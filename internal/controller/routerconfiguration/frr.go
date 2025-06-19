// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/frr"
	"github.com/openperouter/openperouter/internal/frrconfig"
)

type frrConfigData struct {
	configFile string
	address    string
	port       int
	nodeIndex  int
	logLevel   string
	underlays  []v1alpha1.Underlay
	l3vnis     []v1alpha1.L3VNI
}

func configureFRR(ctx context.Context, data frrConfigData) error {
	slog.DebugContext(ctx, "reloading FRR config", "config", data)
	frrConfig, err := conversion.APItoFRR(data.nodeIndex, data.underlays, data.l3vnis, data.logLevel)
	emptyConfig := conversion.FRREmptyConfigError("")
	if errors.As(err, &emptyConfig) {
		slog.InfoContext(ctx, "reloading FRR config", "empty config", data, "event", "cleaning the frr configuration")
		frrConfig = frr.Config{}
	}
	if err != nil && !errors.As(err, &emptyConfig) {
		return fmt.Errorf("failed to generate the frr configuration: %w", err)
	}

	url := fmt.Sprintf("%s:%d", data.address, data.port)
	updater := frrconfig.UpdaterForAddress(url, data.configFile)
	err = frr.ApplyConfig(ctx, &frrConfig, updater)
	if err != nil {
		return fmt.Errorf("failed to update the frr configuration: %w", err)
	}
	return nil
}
