// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
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
		return "", fmt.Errorf("vtysh command failed: %w, output: %s", err, frr.RedactPasswords(string(output)))
	}
	return string(output), nil
}

// clearStaleISIS works around an frr-reload.py ordering bug hit when an `isis passive` is
// removed: frr-reload emits "no isis passive" on an interface after it has
// already detached that interface from the ISIS instance, and FRR rejects the
// command with a YANG "area-tag" error, aborting the reload. Clearing the
// interface-level passive setting while the ISIS instance still exists
// fixes the issue, leading reloads to succeed. See openperouter issue #645
// and upstream FRR issue https://github.com/FRRouting/frr/issues/10133.
func clearStaleISIS(desiredConfigPath string, vtyshRunner func(...string) (string, error)) error {
	running, err := vtyshRunner("show running-config")
	if err != nil {
		return fmt.Errorf("failed to read running config: %w", err)
	}

	runningPassiveInterfaces := isisPassiveInterfaces(running)
	// Nothing to do if we have no running passive interfaces.
	if len(runningPassiveInterfaces) == 0 {
		return nil
	}

	desired, err := os.ReadFile(desiredConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read desired config %s: %w", desiredConfigPath, err)
	}
	desiredPassiveInterfaces := isisPassiveInterfaces(string(desired))

	passiveInterfacesToRemove := map[string]struct{}{}
	for runningPassiveInterface := range runningPassiveInterfaces {
		if _, found := desiredPassiveInterfaces[runningPassiveInterface]; !found {
			passiveInterfacesToRemove[runningPassiveInterface] = struct{}{}
		}
	}

	if len(passiveInterfacesToRemove) == 0 {
		return nil
	}

	commands := isisTeardownCommands(passiveInterfacesToRemove)
	slog.Info("clearing stale ISIS state before reload", "commands", commands)
	if _, err := vtyshRunner(commands...); err != nil {
		return fmt.Errorf("failed to clear stale ISIS state: %w", err)
	}
	return nil
}

func isisPassiveInterfaces(config string) map[string]struct{} {
	passiveInterfaces := map[string]struct{}{}
	currentInterface := ""

	for line := range strings.SplitSeq(config, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "interface "):
			currentInterface = strings.TrimPrefix(trimmed, "interface ")
		case trimmed == "isis passive" && currentInterface != "":
			passiveInterfaces[currentInterface] = struct{}{}
		case trimmed == "exit" || trimmed == "end":
			currentInterface = ""
		}
	}
	return passiveInterfaces
}

func isisTeardownCommands(passiveInterfaces map[string]struct{}) []string {
	commands := make([]string, 0, 2+3*len(passiveInterfaces))
	commands = append(commands, "configure terminal")
	for iface := range passiveInterfaces {
		commands = append(commands,
			fmt.Sprintf("interface %s", iface),
			"no isis passive",
			"exit",
		)
	}
	commands = append(commands, "end")
	return commands
}
