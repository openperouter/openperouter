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

// ValidateGroutL2Trunk rejects the L2Trunks the grout datapath cannot carry
// yet: only SR-IOV trunks are implemented.
func ValidateGroutL2Trunk(trunk v1alpha1.L2Trunk) error {
	if trunk.Spec.Type != v1alpha1.L2TrunkTypeSRIOV {
		return fmt.Errorf("l2trunk %s: type %s is not supported with the grout datapath", trunk.Name, trunk.Spec.Type)
	}
	return nil
}
