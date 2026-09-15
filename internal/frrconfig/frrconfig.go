// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/openperouter/openperouter/internal/frr"
)

const (
	test         = "test"
	reload       = "reload"
	reloaderPath = "/usr/lib/frr/frr-reload.py"
)

// Update reloads the frr configuration at the given path.
func Update(path string) error {
	return update(path, reloadAction)
}

func update(path string, reloadAction func(path string, action string) error) error {
	slog.Info("config update", "path", path)
	err := reloadAction(path, test)
	if err != nil {
		return err
	}
	err = reloadAction(path, reload)
	if err != nil {
		return err
	}
	return nil
}

var execCommand = exec.Command

func reloadAction(path string, action string) error {
	reloadParameter := "--" + action
	cmd := execCommand("python3", reloaderPath, reloadParameter, "--logfile", "/dev/null", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("frr update failed", "action", action, "error", err, "output", frr.RedactPasswords(string(output)))
		return fmt.Errorf("frr update %s failed: %w", action, err)
	}
	slog.Debug("frr update succeeded", "action", action, "output", frr.RedactPasswords(string(output)))
	return nil
}
