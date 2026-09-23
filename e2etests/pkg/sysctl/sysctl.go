// SPDX-License-Identifier:Apache-2.0

package sysctl

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

const (
	NeighDefaultGcThreshMax = "2147483647"
)

// Sysctl represents a sysctl setting to be enabled.
type Sysctl struct {
	Path        string // The sysctl path under /proc/sys/
	Description string // Human-readable description for logging
	Value       string // Value to configure or to read
}

// Ensure enables the given sysctls.
// Each sysctl is checked and only written if not already set to the target value.
func Ensure(sysctls ...Sysctl) error {
	var errs []error
	for _, s := range sysctls {
		if err := ensureSysctl(s); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Read reads the given sysctls and stores the value in Value.
func Read(sysctls ...*Sysctl) error {
	for _, s := range sysctls {
		if err := readSysctl(s); err != nil {
			return err
		}
	}
	return nil
}

func IPv4NeighDefaultGcThresh1(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv4/neigh/default/gc_thresh1",
		Description: "Minimum number of entries to keep. " +
			"Garbage collector will not purge entries if there are fewer than this number.",
		Value: targetValue,
	}
}

func IPv4NeighDefaultGcThresh2(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv4/neigh/default/gc_thresh2",
		Description: "Threshold when garbage collector becomes more aggressive about purging entries. " +
			"Entries older than 5 seconds will be cleared when over this number.",
		Value: targetValue,
	}
}

func IPv4NeighDefaultGcThresh3(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv4/neigh/default/gc_thresh3",
		Description: "Maximum number of non-PERMANENT neighbor entries allowed. " +
			"Increase this when using large numbers of interfaces and when communicating " +
			"with large numbers of directly-connected peers.",
		Value: targetValue,
	}
}

func IPv6NeighDefaultGcThresh1(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv6/neigh/default/gc_thresh1",
		Description: "Minimum number of entries to keep. " +
			"Garbage collector will not purge entries if there are fewer than this number.",
		Value: targetValue,
	}
}

func IPv6NeighDefaultGcThresh2(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv6/neigh/default/gc_thresh2",
		Description: "Threshold when garbage collector becomes more aggressive about purging entries. " +
			"Entries older than 5 seconds will be cleared when over this number.",
		Value: targetValue,
	}
}

func IPv6NeighDefaultGcThresh3(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv6/neigh/default/gc_thresh3",
		Description: "Maximum number of non-PERMANENT neighbor entries allowed. " +
			"Increase this when using large numbers of interfaces and when communicating " +
			"with large numbers of directly-connected peers.",
		Value: targetValue,
	}
}

// ensureSysctl reads the sysctl at the given path and writes the desired value with sudo if it differs
// from the current one.
func ensureSysctl(s Sysctl) error {
	path := "/proc/sys/" + s.Path
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	currentValue := strings.TrimSpace(string(data))
	if currentValue == s.Value {
		return nil
	}

	cmd := []string{"/bin/bash", "-c", fmt.Sprintf("echo %s > %s", s.Value, path)}
	if out, err := exec.Command("sudo", cmd...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to write to %s: %w (%s)", path, err, out)
	}

	data, err = os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	currentValue = strings.TrimSpace(string(data))
	if currentValue != s.Value {
		return fmt.Errorf("failed to set %s to target value %s, current: %s", path, s.Value, currentValue)
	}

	slog.Info("sysctl enabled", "path", s.Path, "value", s.Value)
	return nil
}

// readSysctl reads the sysctl at the given path and writes it to s.Value.
func readSysctl(s *Sysctl) error {
	path := "/proc/sys/" + s.Path
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	currentValue := strings.TrimSpace(string(data))
	s.Value = currentValue

	slog.Info("sysctl read", "path", s.Path, "value", s.Value)
	return nil
}
