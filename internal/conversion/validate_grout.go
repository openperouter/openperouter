// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"syscall"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/grout"
)

func ValidateGroutL3Passthrough(l3Passthrough v1alpha1.L3Passthrough) error {
	return nil
}

func ValidateGroutL3VNI(l3VNI v1alpha1.L3VNI) error {
	return fmt.Errorf("L3VNI resources are not supported when grout datapath is enabled")
}

func ValidateGroutL2VNI(l2VNI v1alpha1.L2VNI) error {
	return fmt.Errorf("L2VNI resources are not supported when grout datapath is enabled")
}

func ValidateGroutUnderlay(underlay v1alpha1.Underlay) error {
	// The grout port name is the interface name with the underlay prefix,
	// so every interface name must leave room for it, regardless of how
	// the interface is provisioned.
	underlayInterfaces, err := underlayInterfacesToHost(underlay.Spec.Interfaces)
	if err != nil {
		return err
	}
	for _, iface := range underlayInterfaces {
		if len(iface.InterfaceName)+len(grout.UnderlayPortNamePrefix) >= syscall.IFNAMSIZ {
			return fmt.Errorf("nic name %s can't be longer than %d characters", iface.InterfaceName,
				syscall.IFNAMSIZ-len(grout.UnderlayPortNamePrefix))
		}
	}
	return nil
}
