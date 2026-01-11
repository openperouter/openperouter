// SPDX-License-Identifier:Apache-2.0

// inspired by https://github.com/metallb/metallb/blob/1245e7810bcad64d4b6f74cc69c68b219afdc0bd/frr-tools/metrics/liveness/liveness.go
package liveness

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/openperouter/openperouter/internal/frr/vtysh"
)

func PingFrr(frrCli vtysh.Cli) error {
	res, err := frrCli("show daemons")
	if err != nil {
		return fmt.Errorf("fail to list FRR daemons: %w", err)
	}

	expected := map[string]struct{}{
		"bfdd":     {},
		"bgpd":     {},
		"staticd":  {},
		"watchfrr": {},
		"zebra":    {},
	}

	runningDaemons := strings.Fields(res)
	for _, d := range runningDaemons {
		delete(expected, d)
	}
	if len(expected) > 0 {
		return fmt.Errorf(
			"some FRR daemons are not running. got: %q missing: %q",
			res,
			strings.Join(slices.Collect(maps.Keys(expected)), ","),
		)
	}
	return nil
}
