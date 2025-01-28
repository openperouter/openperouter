// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"fmt"
	"log/slog"
	"os/exec"
)

type Action string

const (
	Test         Action = "test"
	Reload       Action = "reload"
	reloaderPath        = "/usr/lib/frr/frr-reload.py"
)

// Update reloads the frr configuration at the given path.
func Update(path string) error {
	slog.Info("config update", "path", path)
	err := reloadAction(path, Test)
	if err != nil {
		return err
	}
	err = reloadAction(path, Reload)
	if err != nil {
		return err
	}
	return nil
}

var execCommand = exec.Command

func reloadAction(path string, action Action) error {
	reloadParameter := "--" + string(action)
	cmd := execCommand("python3", reloaderPath, reloadParameter, path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("frr update failed", "action", action, "error", err, "output", string(output))
		return fmt.Errorf("frr update %s failed: %w", action, err)
	}
	slog.Debug("frr update succeeded", "action", action, "output", string(output))
	return nil
}
