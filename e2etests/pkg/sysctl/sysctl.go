// SPDX-License-Identifier:Apache-2.0

package sysctl

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
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
		Description: "The minimum number of entries to keep in the ARP cache. " +
			"The garbage collector will not run if there are fewer than this number of entries in the cache.",
		Value: targetValue,
	}
}

func IPv4NeighDefaultGcThresh2(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv4/neigh/default/gc_thresh2",
		Description: "The soft maximum number of entries to keep in the ARP " +
			"cache.  The garbage collector will allow the number of " +
			"entries to exceed this for 5 seconds before collection will " +
			"be performed",
		Value: targetValue,
	}
}

func IPv4NeighDefaultGcThresh3(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv4/neigh/default/gc_thresh3",
		Description: "The hard maximum number of entries to keep in the ARP " +
			"cache.  The garbage collector will always run if there are " +
			"more than this number of entries in the cache.",
		Value: targetValue,
	}
}

func IPv6NeighDefaultGcThresh1(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv6/neigh/default/gc_thresh1",
		Description: "The minimum number of entries to keep in the ARP cache. " +
			"The garbage collector will not run if there are fewer than this number of entries in the cache.",
		Value: targetValue,
	}
}

func IPv6NeighDefaultGcThresh2(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv6/neigh/default/gc_thresh2",
		Description: "The soft maximum number of entries to keep in the ARP " +
			"cache.  The garbage collector will allow the number of " +
			"entries to exceed this for 5 seconds before collection will " +
			"be performed",
		Value: targetValue,
	}
}

func IPv6NeighDefaultGcThresh3(targetValue string) Sysctl {
	return Sysctl{
		Path: "net/ipv6/neigh/default/gc_thresh3",
		Description: "The hard maximum number of entries to keep in the ARP " +
			"cache.  The garbage collector will always run if there are " +
			"more than this number of entries in the cache.",
		Value: targetValue,
	}
}

// ensureSysctl reads the sysctl at the given path and writes the desired value if it differs
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

	if err := os.WriteFile(path, []byte(s.Value), 0644); err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}
	slog.Info("sysctl enabled", "path", s.Path, "description", s.Description, "value", s.Value)

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

	slog.Info("sysctl read", "path", s.Path, "description", s.Description, "value", s.Value)

	return nil
}
