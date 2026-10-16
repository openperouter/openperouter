// SPDX-License-Identifier:Apache-2.0

// inspired by https://github.com/metallb/metallb/blob/1245e7810bcad64d4b6f74cc69c68b219afdc0bd/frr-tools/metrics/liveness/liveness.go
package liveness

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/openperouter/openperouter/internal/frr/vtysh"
)

func PingFrr(frrCli vtysh.Cli) error {
	res, err := frrCli("show daemons")
	if err != nil {
		return fmt.Errorf("fail to list FRR daemons: %w", err)
	}

	expected := map[string]struct{}{
		"bfdd":     {},
		"bgpd":     {},
		"staticd":  {},
		"watchfrr": {},
		"zebra":    {},
	}

	runningDaemons := strings.FieldsSeq(res)
	for d := range runningDaemons {
		delete(expected, d)
	}
	if len(expected) > 0 {
		return fmt.Errorf(
			"some FRR daemons are not running. got: %q missing: %q",
			res,
			strings.Join(slices.Collect(maps.Keys(expected)), ","),
		)
	}
	return nil
}

// ProcessAlive reports whether the process identified by pid exists.
func ProcessAlive(pid int) error {
	return syscall.Kill(pid, 0)
}

// ErrWatchFRRNotStarted reports that frr-start has not created the watchfrr pid file.
var ErrWatchFRRNotStarted = errors.New("watchfrr has not started")

// WatchFRR reports whether watchfrr is running and responding on its VTY. It
// returns ErrWatchFRRNotStarted when the watchfrr pid file does not exist.
func WatchFRR(pidFilePath string, watchFRRCli vtysh.Cli, processAlive func(int) error) error {
	content, err := os.ReadFile(pidFilePath)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrWatchFRRNotStarted
	}
	if err != nil {
		return fmt.Errorf("failed to read the watchfrr pid file %s: %w", pidFilePath, err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return fmt.Errorf("invalid pid in the watchfrr pid file %s: %w", pidFilePath, err)
	}
	if err := processAlive(pid); err != nil {
		return fmt.Errorf("watchfrr %d is not running: %w", pid, err)
	}
	if _, err := watchFRRCli("show watchfrr"); err != nil {
		return fmt.Errorf("watchfrr %d does not answer on its vty: %w", pid, err)
	}
	return nil
}

type CheckFn func() error
