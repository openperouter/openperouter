// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"os"
	"sync"

	"github.com/go-kit/log"
)

type ConfigUpdater func(context.Context, string) error

type FRR struct {
	sync.Mutex
}

// Create a variable for os.Hostname() in order to make it easy to mock out
// in unit tests.
var osHostname = os.Hostname

func ApplyConfig(ctx context.Context, config *Config, updater ConfigUpdater) error {
	hostname, err := osHostname()
	if err != nil {
		return err
	}

	config.Hostname = hostname
	return generateAndReloadConfigFile(ctx, config, updater)
}

func NewFRR(logger log.Logger) *FRR {
	res := &FRR{}
	return res
}
