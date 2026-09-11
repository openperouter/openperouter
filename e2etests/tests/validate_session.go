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
	"github.com/openperouter/openperouter/e2etests/pkg/networklayerprotocol"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
)

const Established = true

func validateFRRK8sSessionForHostSession(name string, hostsession v1alpha1.HostSession, established bool, frrk8sPods ...*corev1.Pod) {
	cidrs := hostsession.LocalCIDRs

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
	validate.SessionWithNeighbor(exec, validate.SessionParameters{
		FromName:                parameters.fromName,
		ToName:                  parameters.toName,
		NeighborIP:              parameters.neighborIP,
		ReceivedAddressFamilies: parameters.receivedAddressFamilies,
		Established:             parameters.established,
	})
}

type validationParameters struct {
	fromName                string
	toName                  string
	neighborIP              string
	receivedAddressFamilies []networklayerprotocol.NLP
	established             bool
}

func waitForType5Route(exec executor.Executor, prefix string) {
	validate.Type5RouteExists(exec, prefix)
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
