// SPDX-License-Identifier:Apache-2.0

//go:build !linux

package hostnetwork

import "fmt"

func ReconcileSRv6Isolation(_ string, vpns []L3VPNParams, _ ...string) error {
	for _, vpn := range vpns {
		if vpn.RoutingIsolation != nil {
			return fmt.Errorf("SRv6 routing isolation requires Linux")
		}
	}
	return nil
}

func verifySRv6Isolation(_ string, _ SRv6Isolation) error {
	return fmt.Errorf("SRv6 routing isolation requires Linux")
}
