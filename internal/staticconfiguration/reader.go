// SPDX-License-Identifier:Apache-2.0

package staticconfiguration

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openperouter/openperouter/api/static"
	"sigs.k8s.io/yaml"
)

// NoConfigAvailable is returned when no configuration files are available.
type NoConfigAvailable struct {
	message string
}

func (e *NoConfigAvailable) Error() string {
	return e.message
}

// FileExists checks if a file exists at the given path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReadNodeConfig reads a NodeConfig from a YAML file.
// If the file does not exist, returns an empty config.
func ReadNodeConfig(path string) (*static.NodeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read node config file: %w", err)
	}

	var config static.NodeConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML node config: %w", err)
	}

	return &config, nil
}

// ReadRouterConfigs reads all openpe_*.yaml files from a directory.
// Returns NoConfigAvailable error if the directory doesn't exist or contains no matching files.
func ReadRouterConfigs(configDir string) ([]*static.PERouterConfig, error) {
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return nil, &NoConfigAvailable{
			message: fmt.Sprintf("configuration directory does not exist: %s", configDir),
		}
	}

	pattern := filepath.Join(configDir, "openpe_*.yaml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob pattern %s: %w", pattern, err)
	}

	if len(matches) == 0 {
		return nil, &NoConfigAvailable{
			message: fmt.Sprintf("no openpe_*.yaml configuration files found in directory: %s", configDir),
		}
	}

	configs := make([]*static.PERouterConfig, 0, len(matches))
	for _, path := range matches {
		config, err := readRouterConfig(path)
		if err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}

	return configs, nil
}

// readRouterConfig reads a PERouterConfig from a single YAML file.
func readRouterConfig(path string) (*static.PERouterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read router config file %s: %w", path, err)
	}

	var config static.PERouterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML router config %s: %w", path, err)
	}

	return &config, nil
}
