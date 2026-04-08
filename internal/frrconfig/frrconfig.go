// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
)

type Action string

const (
	Test                Action = "test"
	Reload              Action = "reload"
	DefaultReloaderPath        = "/usr/lib/frr/frr-reload.py"
)

// Update reloads the frr configuration at the given path.
func Update(configPath, reloaderPath, vtyshPath string) error {
	slog.Info("config update", "path", configPath)
	err := reloadAction(configPath, reloaderPath, vtyshPath, Test)
	if err != nil {
		return err
	}
	err = reloadAction(configPath, reloaderPath, vtyshPath, Reload)
	if err != nil {
		return err
	}
	return nil
}

var execCommand = exec.Command

func reloadAction(configPath, reloaderPath, vtyshPath string, action Action) error {
	reloadParameter := "--" + string(action)
	cmd := execCommand("python3", reloaderPath, "--bindir", filepath.Dir(vtyshPath), reloadParameter, configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("frr update failed", "action", action, "error", err, "output", string(output))
		return fmt.Errorf("frr update %s failed: %w", action, err)
	}
	slog.Debug("frr update succeeded", "action", action, "output", string(output))
	return nil
}
