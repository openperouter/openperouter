// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
)

func APItoHostConfig(nodeIndex int, targetNS string, underlays []v1alpha1.Underlay, vnis []v1alpha1.VNI) (hostnetwork.UnderlayParams, []hostnetwork.VNIParams, error) {
	if len(underlays) > 1 {
		return hostnetwork.UnderlayParams{}, nil, fmt.Errorf("can't have more than one underlay")
	}
	if len(underlays) == 0 || len(vnis) == 0 {
		return hostnetwork.UnderlayParams{}, nil, nil
	}

	underlay := underlays[0]

	vtepIP, err := ipam.VTEPIp(underlay.Spec.VTEPCIDR, nodeIndex)
	if err != nil {
		return hostnetwork.UnderlayParams{}, nil, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIntex %d", underlay.Spec.VTEPCIDR, nodeIndex)
	}

	underlayParams := hostnetwork.UnderlayParams{
		UnderlayInterface: underlay.Spec.Nics[0],
		TargetNS:          targetNS,
		VtepIP:            vtepIP.IP.String(),
	}

	vniParams := []hostnetwork.VNIParams{}

	for _, vni := range vnis {
		vethIPs, err := ipam.VethIPs(vni.Spec.LocalCIDR, nodeIndex)
		if err != nil {
			return hostnetwork.UnderlayParams{}, nil, fmt.Errorf("failed to get veth ips, cidr %s, nodeIndex %d", vni.Spec.LocalCIDR, nodeIndex)
		}

		v := hostnetwork.VNIParams{
			VRF:        *vni.Spec.VRF,
			TargetNS:   targetNS,
			VTEPIP:     vtepIP.IP.String(),
			VNI:        int(vni.Spec.VNI),
			VethHostIP: vethIPs.HostSide.String(),
			VethNSIP:   vethIPs.ContainerSide.String(),
			VXLanPort:  int(vni.Spec.VXLanPort),
		}
		vniParams = append(vniParams, v)
	}

	return underlayParams, vniParams, nil
}
