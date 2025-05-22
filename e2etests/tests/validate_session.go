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

func validateFRRK8sSessionForVNI(vni v1alpha1.VNI, established bool, frrk8sPods ...*corev1.Pod) {
	neighborIP, err := openperouter.RouterIPFromCIDR(vni.Spec.LocalCIDR)
	Expect(err).NotTo(HaveOccurred())

	for _, p := range frrk8sPods {
		By(fmt.Sprintf("checking the session between %s and vni %s", p.Name, vni.Name))
		exec := executor.ForPod(p.Namespace, p.Name, "frr")
		validateSessionWithNeighbor(p.Name, vni.Name, exec, neighborIP, established)
	}
}

func validateSessionWithNeighbor(fromName, toName string, exec executor.Executor, neighborIP string, established bool) {
	Eventually(func() error {
		neigh, err := frr.NeighborInfo(neighborIP, exec)
		if err != nil {
			return err
		}
		if !established && neigh.BgpState == "Established" {
			return fmt.Errorf("neighbor %s is established", neighborIP)
		}
		if established && neigh.BgpState != "Established" {
			return fmt.Errorf("neighbor %s is not established", neighborIP)
		}
		return nil
	}, 5*time.Minute, time.Second).ShouldNot(HaveOccurred())
}

func validateNoSuchNeigh(exec executor.Executor, neighborIP string) {
	Eventually(func() error {
		neigh, err := frr.NeighborInfo(neighborIP, exec)
		if errors.As(err, &frr.NoNeighborError{}) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("neighbor %s exists: %v", neighborIP, neigh)
	}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
}
