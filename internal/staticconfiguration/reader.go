// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"fmt"
	"os"

	"github.com/openperouter/openperouter/api/static"
	"sigs.k8s.io/yaml"
)

// ReadFromFile reads a PERouterConfig from a YAML file.
func ReadFromFile(path string) (*static.PERouterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config static.PERouterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	return &config, nil
}
