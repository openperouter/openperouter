// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
)

const Established = true

func validateFRRK8sSessionForVNI(vni v1alpha1.L3VNI, established bool, frrk8sPods ...*corev1.Pod) {
	var cidrs []string
	Expect(vni.Spec.HostSession != nil).To(BeTrue(), "HostSession must be provided")
	Expect(vni.Spec.HostSession.LocalCIDR.IPv4 != "" || vni.Spec.HostSession.LocalCIDR.IPv6 != "").To(BeTrue(), "either IPv4 or IPv6 CIDR must be provided")

	if vni.Spec.HostSession.LocalCIDR.IPv4 != "" {
		cidrs = append(cidrs, vni.Spec.HostSession.LocalCIDR.IPv4)
	}
	if vni.Spec.HostSession.LocalCIDR.IPv6 != "" {
		cidrs = append(cidrs, vni.Spec.HostSession.LocalCIDR.IPv6)
	}

	for _, cidr := range cidrs {
		neighborIP, err := openperouter.RouterIPFromCIDR(cidr)
		Expect(err).NotTo(HaveOccurred())

		for _, p := range frrk8sPods {
			By(fmt.Sprintf("checking the session between %s and vni %s for CIDR %s", p.Name, vni.Name, cidr))
			exec := executor.ForPod(p.Namespace, p.Name, "frr")
			validateSessionWithNeighbor(p.Name, vni.Name, exec, neighborIP, established)
		}
	}
}

func validateSessionWithNeighbor(fromName, toName string, exec executor.Executor, neighborIP string, established bool) {
	Eventually(func() error {
		neigh, err := frr.NeighborInfo(neighborIP, exec)
		if err != nil {
			return err
		}
		if !established && neigh.BgpState == "Established" {
			return fmt.Errorf("neighbor from %s to %s - %s is established", fromName, toName, neighborIP)
		}
		if established && neigh.BgpState != "Established" {
			return fmt.Errorf("neighbor %s to %s - %s is not established", fromName, toName, neighborIP)
		}
		return nil
	}, 5*time.Minute, time.Second).ShouldNot(HaveOccurred())
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
	}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
}
