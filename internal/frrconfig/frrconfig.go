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

type Action string

const (
	Test         Action = "test"
	Reload       Action = "reload"
	reloaderPath        = "/usr/lib/frr/frr-reload.py"
	vtyshPath           = "/usr/bin/vtysh"
)

// Update reloads the frr configuration at the given path.
func Update(path string) error {
	slog.Info("config update", "path", path)
	if err := reloadAction(path, Test); err != nil {
		return err
	}
	if err := clearStaleISIS(path); err != nil {
		return err
	}
	if err := reloadAction(path, Reload); err != nil {
		return err
	}
	return nil
}

var execCommand = exec.Command

func reloadAction(path string, action Action) error {
	reloadParameter := "--" + string(action)
	cmd := execCommand("python3", reloaderPath, reloadParameter, "--logfile", "/dev/null", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("frr update failed", "action", action, "error", err, "output", frr.RedactPasswords(string(output)))
		return fmt.Errorf("frr update %s failed: %w", action, err)
	}
	slog.Debug("frr update succeeded", "action", action, "output", frr.RedactPasswords(string(output)))
	return nil
}

// clearStaleISIS removes ISIS from the running FRR state before a reload that
// drops the ISIS underlay, working around an frr-reload.py ordering bug (see
// needsISISTeardown). It is a no-op unless the running config has ISIS and the
// desired config does not.
func clearStaleISIS(desiredConfigPath string) error {
	running, err := runVtysh("show running-config")
	if err != nil {
		return fmt.Errorf("failed to read running config: %w", err)
	}
	// Skip reading the desired config when there is no ISIS instance to remove.
	if !hasISISInstance(running) {
		return nil
	}

	desired, err := os.ReadFile(desiredConfigPath)
	if err != nil {
		return fmt.Errorf("failed to read desired config %s: %w", desiredConfigPath, err)
	}

	commands := needsISISTeardown(running, string(desired))
	if len(commands) == 0 {
		return nil
	}

	slog.Info("clearing stale ISIS state before reload", "commands", commands)
	if _, err := runVtysh(commands...); err != nil {
		return fmt.Errorf("failed to clear stale ISIS state: %w", err)
	}
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

// needsISISTeardown returns the ordered vtysh commands required to remove ISIS
// from the running FRR state, or nil when no teardown is needed.
//
// It works around an frr-reload.py ordering bug hit when an ISIS underlay is
// removed: frr-reload emits "no isis passive" on an interface after it has
// already detached that interface from the ISIS instance, and FRR rejects the
// command with a YANG "area-tag" error, aborting the reload. Clearing the
// interface-level passive setting while the ISIS instance still exists, then
// removing the instance, leaves isisd matching the ISIS-free config so the
// following reload succeeds. See openperouter issue #645 and upstream FRR issue
// https://github.com/FRRouting/frr/issues/10133.
func needsISISTeardown(runningConfig, desiredConfig string) []string {
	if !hasISISInstance(runningConfig) {
		return nil
	}
	if hasISISInstance(desiredConfig) {
		return nil
	}
	return isisTeardownCommands(runningConfig)
}

func isisTeardownCommands(runningConfig string) []string {
	processNames := []string{}
	passiveInterfaces := []string{}
	currentInterface := ""

	for line := range strings.SplitSeq(runningConfig, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "interface "):
			currentInterface = strings.TrimPrefix(trimmed, "interface ")
		case strings.HasPrefix(trimmed, "router isis "):
			processNames = append(processNames, strings.TrimPrefix(trimmed, "router isis "))
			currentInterface = ""
		case trimmed == "isis passive" && currentInterface != "":
			passiveInterfaces = append(passiveInterfaces, currentInterface)
		case trimmed == "exit" || trimmed == "!":
			currentInterface = ""
		}
	}

	if len(processNames) == 0 {
		return nil
	}

	commands := []string{"configure terminal"}
	for _, iface := range passiveInterfaces {
		commands = append(commands,
			fmt.Sprintf("interface %s", iface),
			"no isis passive",
			"exit",
		)
	}
	for _, name := range processNames {
		commands = append(commands, fmt.Sprintf("no router isis %s", name))
	}
	commands = append(commands, "end")
	return commands
}

// hasISISInstance reports whether the config declares a top-level "router isis"
// instance. It matches on the instance stanza only, not interface-level
// "ip router isis" / "ipv6 router isis" association lines.
func hasISISInstance(config string) bool {
	for line := range strings.SplitSeq(config, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "router isis ") {
			return true
		}
	}
	return false
}
