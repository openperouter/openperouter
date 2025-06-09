// SPDX-License-Identifier:Apache-2.0

package conversion

import (
	"fmt"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/ipam"
)

func APItoHostConfig(nodeIndex int, targetNS string, underlays []v1alpha1.Underlay, vnis []v1alpha1.VNI, l2vnis []v1alpha1.L2VNI) (hostnetwork.UnderlayParams, []hostnetwork.L3VNIParams, []hostnetwork.L2VNIParams, error) {
	if len(underlays) > 1 {
		return hostnetwork.UnderlayParams{}, nil, nil, fmt.Errorf("can't have more than one underlay")
	}
	if len(underlays) == 0 {
		return hostnetwork.UnderlayParams{}, nil, nil, nil
	}

	underlay := underlays[0]

	vtepIP, err := ipam.VTEPIp(underlay.Spec.VTEPCIDR, nodeIndex)
	if err != nil {
		return hostnetwork.UnderlayParams{}, nil, nil, fmt.Errorf("failed to get vtep ip, cidr %s, nodeIntex %d", underlay.Spec.VTEPCIDR, nodeIndex)
	}

	underlayParams := hostnetwork.UnderlayParams{
		UnderlayInterface: underlay.Spec.Nics[0],
		TargetNS:          targetNS,
		VtepIP:            vtepIP.String(),
	}

	vniParams := []hostnetwork.L3VNIParams{}

	for _, vni := range vnis {
		vethIPs, err := ipam.VethIPs(vni.Spec.LocalCIDR, nodeIndex)
		if err != nil {
			return hostnetwork.UnderlayParams{}, nil, nil, fmt.Errorf("failed to get veth ips, cidr %s, nodeIndex %d", vni.Spec.LocalCIDR, nodeIndex)
		}

		v := hostnetwork.L3VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:       vni.VRFName(),
				TargetNS:  targetNS,
				VTEPIP:    vtepIP.String(),
				VNI:       int(vni.Spec.VNI),
				VXLanPort: int(vni.Spec.VXLanPort),
			},
			VethHostIP: vethIPs.HostSide.String(),
			VethNSIP:   vethIPs.PeSide.String(),
		}
		vniParams = append(vniParams, v)
	}

	l2vniParams := []hostnetwork.L2VNIParams{}
	for _, l2vni := range l2vnis {
		vni := hostnetwork.L2VNIParams{
			VNIParams: hostnetwork.VNIParams{
				VRF:       l2vni.VRFName(),
				TargetNS:  targetNS,
				VTEPIP:    vtepIP.String(),
				VNI:       int(l2vni.Spec.VNI),
				VXLanPort: int(l2vni.Spec.VXLanPort),
			},
		}
		if l2vni.Spec.L2GatewayIP != "" {
			vni.L2GatewayIP = &l2vni.Spec.L2GatewayIP
		}
		if l2vni.Spec.HostMaster != nil {
			vni.HostMaster = &hostnetwork.HostMaster{
				Name:       l2vni.Spec.HostMaster.Name,
				AutoCreate: l2vni.Spec.HostMaster.AutoCreate,
			}
		}

		l2vniParams = append(l2vniParams, vni)
	}

	return underlayParams, vniParams, l2vniParams, nil
}
