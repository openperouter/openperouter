// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"

	"github.com/openperouter/openperouter/internal/hostnetwork"
)

// VNIParams is an alias for hostnetwork.VNIParams, used by the grout dataplane.
type VNIParams = hostnetwork.VNIParams

// SetupL3VNI configures an L3 VNI via the grout dataplane.
func SetupL3VNI(ctx context.Context, params hostnetwork.L3VNIParams) error {
	// TODO: implement grout-based L3 VNI setup
	return nil
}

// SetupL2VNI configures an L2 VNI via the grout dataplane.
func SetupL2VNI(ctx context.Context, params hostnetwork.L2VNIParams) error {
	// TODO: implement grout-based L2 VNI setup
	return nil
}

// RemoveAllVNIs removes all VNI configuration from the given namespace.
func RemoveAllVNIs(targetNS string) error {
	// TODO: implement grout-based VNI removal
	return nil
}

// RemoveNonConfiguredVNIs removes VNIs that are present in the namespace but not in the provided list.
func RemoveNonConfiguredVNIs(targetNS string, params []VNIParams) error {
	// TODO: implement grout-based stale VNI cleanup
	return nil
}
