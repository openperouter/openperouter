// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"testing"

	"github.com/openperouter/openperouter/internal/grout"
	"github.com/stretchr/testify/require"
)

func TestDatapathRouterMACUsesCurrentNodeIndex(t *testing.T) {
	const prefix = "02:00:00:00:00:00/32"
	tests := []struct {
		name         string
		configurator DatapathConfigurator
	}{
		{name: "kernel", configurator: &KernelDatapathConfigurator{RouterMACPrefix: prefix}},
		{name: "grout", configurator: NewGroutConfigurator("", grout.PortModeTAP, prefix)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reuse the configurator so each reconciliation must read the current index.
			for _, index := range []int{65536, -1} {
				err := tt.configurator.Configure(context.Background(), interfacesConfiguration{nodeIndex: index})
				require.EqualError(t, err, fmt.Sprintf(
					"failed to generate router MAC: node index %d does not fit router MAC prefix %q", index, prefix))
			}
		})
	}
}
