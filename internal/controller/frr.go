// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
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
	vnis       []v1alpha1.VNI
}

func reloadFRRConfig(ctx context.Context, data frrConfigData) error {
	slog.DebugContext(ctx, "reloading FRR config", "config", data)
	frrConfig, err := conversion.APItoFRR(data.nodeIndex, data.underlays, data.vnis, data.logLevel)
	if err != nil {
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
