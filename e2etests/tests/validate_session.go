// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"errors"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/networklayerprotocol"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const Established = true

func validateFRRK8sSessionForHostSession(name string, hostsession v1alpha1.HostSession, established bool, frrk8sPods ...*corev1.Pod) {
	var cidrs []string

	if ipv4CIDR := ptr.Deref(hostsession.LocalCIDR.IPv4, ""); ipv4CIDR != "" {
		cidrs = append(cidrs, ipv4CIDR)
	}
	if ipv6CIDR := ptr.Deref(hostsession.LocalCIDR.IPv6, ""); ipv6CIDR != "" {
		cidrs = append(cidrs, ipv6CIDR)
	}

	Expect(cidrs).NotTo(BeEmpty(), "either IPv4 or IPv6 CIDR must be provided")

	for _, cidr := range cidrs {
		neighborIP, err := openperouter.RouterIPFromCIDR(cidr)
		Expect(err).NotTo(HaveOccurred())

		for _, p := range frrk8sPods {
			By(fmt.Sprintf("checking the session between %s and session %s for CIDR %s", p.Name, name, cidr))
			exec := executor.ForPod(p.Namespace, p.Name, "frr")
			validateSessionWithNeighbor(
				exec,
				validationParameters{
					fromName:    p.Name,
					toName:      name,
					neighborIP:  neighborIP,
					established: established,
				},
			)
		}
	}
}

func validateSessionWithNeighbor(exec executor.Executor, parameters validationParameters) {
	Eventually(func() error {
		neigh, err := frr.NeighborInfo(parameters.neighborIP, exec)
		if err != nil {
			return err
		}
		if !parameters.established && neigh.BgpState == "Established" {
			return fmt.Errorf("neighbor from %s to %s - %s is established", parameters.fromName, parameters.toName, parameters.neighborIP)
		}
		if parameters.established && neigh.BgpState != "Established" {
			return fmt.Errorf("neighbor %s to %s - %s is not established", parameters.fromName, parameters.toName, parameters.neighborIP)
		}

		// receivedAddressFamilies check is optional and will be skipped if the slice is empty or established == false.
		if !parameters.established {
			return nil
		}
		for _, expectedReceivedAF := range parameters.receivedAddressFamilies {
			isRxReceived := false
			for pathName, addPath := range neigh.NeighborCapabilities.AddPath {
				if strings.ToLower(pathName) == fmt.Sprintf("%s%s", expectedReceivedAF.AFI, expectedReceivedAF.SAFI) {
					isRxReceived = addPath.RxReceived
					break
				}
			}
			if isRxReceived {
				continue
			}
			return fmt.Errorf("neighbor %s to %s - %s is established but expectedReceivedAF %s not found",
				parameters.fromName, parameters.toName, parameters.neighborIP, expectedReceivedAF)
		}

		return nil
	}, 5*time.Minute, time.Second).ShouldNot(HaveOccurred())
}

type validationParameters struct {
	fromName                string
	toName                  string
	neighborIP              string
	receivedAddressFamilies []networklayerprotocol.NLP
	established             bool
}

func waitForType5Route(exec executor.Executor, prefix string) {
	Eventually(func() error {
		evpn, err := frr.EVPNInfo(exec)
		if err != nil {
			return err
		}
		if !evpn.ContainsType5Prefix(prefix) {
			return fmt.Errorf("Type-5 route for %s not yet present", prefix)
		}
		return nil
	}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
}

// validateSessionDownForNeigh validates that the neighbor is down
// or if the session does not exist.
func validateSessionDownForNeigh(exec executor.Executor, neighborIP string) {
	Eventually(func() error {
		neigh, err := frr.NeighborInfo(neighborIP, exec)
		if errors.As(err, &frr.NoNeighborError{}) {
			return nil
		}
		if err != nil {
			return err
		}

		if neigh.BgpState == "Established" {
			return fmt.Errorf("neighbor %s is established: %v", neighborIP, neigh)
		}
		return nil
	}, 5*time.Minute, time.Second).ShouldNot(HaveOccurred())
}
