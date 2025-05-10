// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"errors"
	"fmt"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

func RawDump(exec executor.Executor) (string, error) {
	allerrs := errors.New("")
	res := "####### Show version\n"
	out, err := exec.Exec("vtysh", "-c", "show version")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec show version: %v", err))
	}
	res += out

	res += "####### Show running config\n"
	out, err = exec.Exec("vtysh", "-c", "show running-config")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec show bgp neighbor: %v", err))
	}
	res += out

	res += "####### BGP Neighbors\n"
	out, err = exec.Exec("vtysh", "-c", "show bgp vrf all neighbor")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec show bgp neighbor: %v", err))
	}
	res += out

	res += "####### Routes\n"
	out, err = exec.Exec("vtysh", "-c", "show bgp vrf all ipv4")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec show bgp ipv4: %v", err))
	}
	res += out

	res += "####### Routes\n"
	out, err = exec.Exec("vtysh", "-c", "show bgp vrf all ipv6")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec show bgp ipv6: %v", err))
	}
	res += out

	res += "####### EVPN Routes\n"
	out, err = exec.Exec("vtysh", "-c", "show bgp l2vpn evpn")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec show bgp ipv6: %v", err))
	}
	res += out

	res += "####### Network setup for host\n"
	out, err = exec.Exec("bash", "-c", "ip -6 route; ip -4 route")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec to print network setup: %v", err))
	}
	res += out
	out, err = exec.Exec("bash", "-c", "ip l")
	if err != nil {
		allerrs = errors.Join(allerrs, fmt.Errorf("\nFailed exec to print network setup: %v", err))
	}
	res += out

	if allerrs.Error() == "" {
		allerrs = nil
	}

	return res, allerrs
}
