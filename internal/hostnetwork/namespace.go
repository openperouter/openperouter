// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/vishvananda/netns"
)

// inNamespace execs the provided function in the given network
// namespace.
func inNamespace(ns netns.NsHandle, execInNamespace func() error) error {
	// required as a change of context might wake up the goroutine
	// in a different thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origns, err := netns.Get()
	if err != nil {
		return fmt.Errorf("setupUnderlay: Failed to get current network namespace")
	}
	allErrors := []error{}
	defer deferWithError(&allErrors, func() error { return origns.Close() })

	err = netns.Set(ns)
	if err != nil {
		allErrors = append(allErrors, fmt.Errorf("setupUnderlay: Failed to set current network namespace to %s", ns.String()))
		return errors.Join(allErrors...)
	}
	defer deferWithError(&allErrors, func() error { return netns.Set(origns) })

	err = execInNamespace()
	if err != nil {
		allErrors = append(allErrors, err)
		return errors.Join(allErrors...)
	}
	if err := errors.Join(allErrors...); err != nil {
		return err
	}
	return nil
}
