// SPDX-License-Identifier:Apache-2.0

package validate

import (
	"fmt"
	"strings"
	"time"

	"github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/networklayerprotocol"
)

type SessionParameters struct {
	FromName                string
	ToName                  string
	NeighborIP              string
	ReceivedAddressFamilies []networklayerprotocol.NLP
	Established             bool
}

func SessionWithNeighbor(exec executor.Executor, parameters SessionParameters) {
	gomega.EventuallyWithOffset(1, func() error {
		neigh, err := frr.NeighborInfo(parameters.NeighborIP, exec)
		if err != nil {
			return err
		}
		if !parameters.Established && neigh.BgpState == "Established" {
			return fmt.Errorf("neighbor from %s to %s - %s is established", parameters.FromName, parameters.ToName, parameters.NeighborIP)
		}
		if parameters.Established && neigh.BgpState != "Established" {
			return fmt.Errorf("neighbor %s to %s - %s is not established", parameters.FromName, parameters.ToName, parameters.NeighborIP)
		}

		if !parameters.Established {
			return nil
		}
		for _, expectedReceivedAF := range parameters.ReceivedAddressFamilies {
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
				parameters.FromName, parameters.ToName, parameters.NeighborIP, expectedReceivedAF)
		}

		return nil
	}, 5*time.Minute, time.Second).Should(gomega.Succeed())
}

func Type5RouteExists(exec executor.Executor, prefix string) {
	gomega.EventuallyWithOffset(1, func() error {
		evpn, err := frr.EVPNInfo(exec)
		if err != nil {
			return err
		}
		if !evpn.ContainsType5Prefix(prefix) {
			return fmt.Errorf("Type-5 route for %s not yet present", prefix)
		}
		return nil
	}, 2*time.Minute, time.Second).Should(gomega.Succeed())
}
