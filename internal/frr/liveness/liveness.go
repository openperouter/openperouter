// SPDX-License-Identifier:Apache-2.0

package liveness

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/openperouter/openperouter/internal/frr/vtysh"
)

// ProcessAlive reports whether the process identified by pid exists.
func ProcessAlive(pid int) error {
	return syscall.Kill(pid, 0)
}

type CheckFn func() error

// WatchFRR reports whether watchfrr is running and responding on its VTY.
func WatchFRR(pidFilePath string, watchFRRCli vtysh.Cli, processAlive func(int) error) error {
	_, err := watchFRROutput(pidFilePath, watchFRRCli, processAlive)
	return err
}

// WatchFRRReady reports whether every daemon supervised by watchfrr is Up.
func WatchFRRReady(pidFilePath string, watchFRRCli vtysh.Cli, processAlive func(int) error) error {
	output, err := watchFRROutput(pidFilePath, watchFRRCli, processAlive)
	if err != nil {
		return err
	}
	return checkWatchFRRDaemons(output)
}

func watchFRROutput(pidFilePath string, watchFRRCli vtysh.Cli, processAlive func(int) error) (string, error) {
	content, err := os.ReadFile(pidFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read the watchfrr pid file %s: %w", pidFilePath, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return "", fmt.Errorf("invalid pid in the watchfrr pid file %s: %w", pidFilePath, err)
	}
	if err := processAlive(pid); err != nil {
		return "", fmt.Errorf("watchfrr %d is not running: %w", pid, err)
	}
	monitorOutput, err := watchFRRCli("show watchfrr")
	if err != nil {
		return "", fmt.Errorf("watchfrr %d does not answer on its vty: %w", pid, err)
	}
	return monitorOutput, nil
}

func checkWatchFRRDaemons(output string) error {
	unhealthyDaemons := []string{}
	reportedDaemons := false
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		reportedDaemons = true
		if fields[1] == "Up" {
			continue
		}
		unhealthyDaemons = append(unhealthyDaemons, fmt.Sprintf("%s is %s", fields[0], fields[1]))
	}

	if !reportedDaemons {
		return errors.New("watchfrr did not report any daemon states")
	}
	if len(unhealthyDaemons) > 0 {
		return fmt.Errorf("watchfrr reports unhealthy daemons: %s", strings.Join(unhealthyDaemons, ", "))
	}
	return nil
}
