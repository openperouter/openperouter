// SPDX-License-Identifier:Apache-2.0

//go:build !linux

package hostnetwork

import (
	"fmt"
	"net"
)

func StageSRv6Isolation(targetNS string, vpns []L3VPNParams, source net.IP, tenantVRFs ...string) error {
	return CheckSRv6IsolationPlan(targetNS, vpns, source, tenantVRFs...)
}

func ReconcileSRv6Isolation(_ string, vpns []L3VPNParams, _ ...string) error {
	for _, vpn := range vpns {
		if vpn.RoutingIsolation != nil {
			return fmt.Errorf("SRv6 routing isolation requires Linux")
		}
	}
	return nil
}

func CheckSRv6IsolationPlan(_ string, vpns []L3VPNParams, _ net.IP, _ ...string) error {
	for _, vpn := range vpns {
		if vpn.RoutingIsolation != nil {
			return fmt.Errorf("SRv6 routing isolation requires Linux")
		}
	}
	return nil
}

func PrepareSRv6Isolation(targetNS string, vpns []L3VPNParams, _ net.IP, tenantVRFs ...string) error {
	return ReconcileSRv6Isolation(targetNS, vpns, tenantVRFs...)
}

func FinishSRv6Isolation(targetNS string, vpns []L3VPNParams, _ net.IP, tenantVRFs ...string) error {
	return ReconcileSRv6Isolation(targetNS, vpns, tenantVRFs...)
}

func verifySRv6Isolation(_ string, _ SRv6Isolation) error {
	return fmt.Errorf("SRv6 routing isolation requires Linux")
}
