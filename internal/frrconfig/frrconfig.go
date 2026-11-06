// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/openperouter/openperouter/internal/frr"
)

const (
	test         = "test"
	reload       = "reload"
	reloaderPath = "/usr/lib/frr/frr-reload.py"
	vtyshPath    = "/usr/bin/vtysh"
)

// Update reloads the frr configuration at the given path.
func Update(path string) error {
	return update(path, reloadAction, runVtysh)
}

func update(
	path string,
	reloadAction func(path string, action string) error,
	vtyshRunner func(...string) (string, error),
) error {
	slog.Info("config update", "path", path)
	if err := clearStaleISIS(path, vtyshRunner); err != nil {
		return err
	}
	if err := reloadAction(path, test); err != nil {
		return err
	}
	return reloadAction(path, reload)
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

func runVtysh(commands ...string) (string, error) {
	args := make([]string, 0, len(commands)*2)
	for _, c := range commands {
		args = append(args, "-c", c)
	}
	cmd := execCommand(vtyshPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"vtysh command failed: cmd: %q, output: %q, err: %w",
			frr.RedactPasswords(strings.Join(commands, "\n")),
			frr.RedactPasswords(string(output)),
			err,
		)
	}
	slog.Debug(
		"frr update ran vtysh command",
		"command", frr.RedactPasswords(strings.Join(commands, "\n")),
		"output", frr.RedactPasswords(string(output)),
	)
	return string(output), nil
}

// clearStaleISIS works around an frr-reload.py ordering bug hit when
// interface-level ISIS settings such as `isis passive` or
// `isis network point-to-point` are removed: frr-reload emits the "no"
// form of the setting on an interface after it has already detached that
// interface from the ISIS instance, and FRR rejects the command with a YANG
// "area-tag" error, aborting the reload. Clearing the interface-level
// settings while the ISIS instance still exists fixes the issue, leading
// reloads to succeed. See openperouter issue #645 and upstream FRR issue
// https://github.com/FRRouting/frr/issues/10133.
func clearStaleISIS(desiredConfigPath string, vtyshRunner func(...string) (string, error)) error {
	running, err := vtyshRunner("show running-config")
	if err != nil {
		return fmt.Errorf("failed to read running config: %w", err)
	}

	runningSettings := isisInterfaceSettings(running)
	// Nothing to do if we have no running interface-level ISIS settings.
	if len(runningSettings) == 0 {
		return nil
	}

	desired, err := os.ReadFile(desiredConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read desired config %s: %w", desiredConfigPath, err)
	}
	desiredSettings := isisInterfaceSettings(string(desired))

	settingsToRemove := map[string][]string{}
	for iface, runningCmds := range runningSettings {
		desiredCmds := desiredSettings[iface]
		for _, cmd := range runningCmds {
			if !slices.Contains(desiredCmds, cmd) {
				settingsToRemove[iface] = append(settingsToRemove[iface], cmd)
			}
		}
	}

	if len(settingsToRemove) == 0 {
		return nil
	}

	commands := isisTeardownCommands(settingsToRemove)
	slog.Info("clearing stale ISIS state before reload", "commands", commands)
	if _, err := vtyshRunner(commands...); err != nil {
		return fmt.Errorf("failed to clear stale ISIS state: %w", err)
	}
	return nil
}

// isisInterfaceSettings scans an FRR config and returns, per interface, the
// interface-level ISIS settings that frr-reload cannot remove once the
// interface has been detached from the ISIS instance.
// e.g.
// [lo] -> [isis passive]
// [toleafkind1] -> [isis network point-to-point, isis passive]
func isisInterfaceSettings(config string) map[string][]string {
	settings := map[string][]string{}
	currentInterface := ""

	for line := range strings.SplitSeq(config, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "interface "):
			currentInterface = strings.TrimPrefix(trimmed, "interface ")
		case currentInterface != "" && isisInterfaceLevelSetting(trimmed):
			settings[currentInterface] = append(settings[currentInterface], trimmed)
		case trimmed == "exit" || trimmed == "end":
			currentInterface = ""
		}
	}
	return settings
}

// isisInterfaceLevelSetting returns true for interface-level ISIS commands
// affected by the frr-reload removal ordering bug.
func isisInterfaceLevelSetting(line string) bool {
	return line == "isis passive" || line == "isis network point-to-point"
}

func isisTeardownCommands(settings map[string][]string) []string {
	commands := []string{"configure terminal"}
	for _, iface := range slices.Sorted(maps.Keys(settings)) {
		commands = append(commands, fmt.Sprintf("interface %s", iface))
		for _, cmd := range settings[iface] {
			commands = append(commands, "no "+cmd)
		}
		commands = append(commands, "exit")
	}
	return append(commands, "end")
}
