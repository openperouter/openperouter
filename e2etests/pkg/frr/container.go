// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

const frrConfig = "/etc/frr/frr.conf.new"

type Container struct {
	Name       string
	ConfigPath string
}

// ReloadConfig reloads the FRR configuration in the container.
func (c Container) ReloadConfig(config string) error {
	if err := writeFRRConfig(c.Name, config); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", c.ConfigPath, err)
	}
	exec := executor.ForContainer(c.Name)
	// Checking the configuration file syntax.
	cmd := fmt.Sprintf("python3 /usr/lib/frr/frr-reload.py --test --stdout %s", frrConfig)
	out, err := exec.Exec("sh", "-c", cmd)
	if err != nil {
		return errors.Join(err, fmt.Errorf("failed to check configuration file. %s", out))
	}

	// Applying the configuration file.
	cmd = fmt.Sprintf("python3 /usr/lib/frr/frr-reload.py --reload --overwrite --stdout %s", frrConfig)
	out, err = exec.Exec("sh", "-c", cmd)
	if err != nil {
		return errors.Join(err, fmt.Errorf("failed to apply configuration file. %s", out))
	}

	return nil
}

func writeFRRConfig(containerName, config string) error {
	tmpFile, err := os.CreateTemp("", "temp-frr-*.conf")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}

	if _, err := tmpFile.WriteString(config); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	cmd := exec.Command(executor.ContainerRuntime, "cp", tmpFile.Name(), fmt.Sprintf("%s:%s", containerName, frrConfig))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy file to container: %s, %w", string(output), err)
	}

	return nil
}
