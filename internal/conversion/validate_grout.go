// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"
	"syscall"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/grout"
)

func ValidateGroutUnderlay(underlay v1alpha1.Underlay) error {
	for _, iface := range underlay.Spec.Interfaces {
		if iface.Type == v1alpha1.UnderlayInterfaceTypeCNIDevice {
			return fmt.Errorf("CNI dev underlays are not supported with the grout datapath")
		}
	}

	underlayInterfaces, err := underlayInterfacesToHost(underlay.Spec.Interfaces)
	if err != nil {
		return err
	}
	for _, iface := range underlayInterfaces {
		if portName := grout.UnderlayPortName(iface.InterfaceName); len(portName) >= syscall.IFNAMSIZ {
			return fmt.Errorf("nic name %s can't be longer than %d characters, as its grout port name %s "+
				"adds the %q prefix",
				iface.InterfaceName, syscall.IFNAMSIZ-1-len(grout.UnderlayPortNamePrefix), portName,
				grout.UnderlayPortNamePrefix)
		}
	}
	return nil
}

// ValidateGroutL2Trunk rejects the L2Trunks the grout datapath cannot carry:
// only SR-IOV trunks are supported, and the VF index must fit the two hex
// digits reserved in trunk port and VLAN sub-interface names.
func ValidateGroutL2Trunk(trunk v1alpha1.L2Trunk) error {
	if trunk.Spec.Type != v1alpha1.L2TrunkTypeSRIOV {
		return fmt.Errorf("l2trunk %s: type %s is not supported with the grout datapath", trunk.Name, trunk.Spec.Type)
	}
	if trunk.Spec.SRIOV == nil || trunk.Spec.SRIOV.VFIndex == nil {
		return nil // reported by the general L2Trunk validation
	}
	if *trunk.Spec.SRIOV.VFIndex > grout.MaxTrunkVFIndex {
		return fmt.Errorf("l2trunk %s: vfIndex %d is above %d, the maximum for two-digit hexadecimal VF indexes in grout interface names",
			trunk.Name, *trunk.Spec.SRIOV.VFIndex, grout.MaxTrunkVFIndex)
	}
	return nil
}
