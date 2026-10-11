// SPDX-License-Identifier:Apache-2.0

package qemu_e2e

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/triage"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	Updater         *config.Updater
	ReportPath      string
	inspectReporter *k8s.InspectReporter
)

var GroutSupport = ginkgo.Label("grout-support")
var QEMUSupport = ginkgo.Label("qemu-support")

const Established = true

func dumpIfFails(cs clientset.Interface, additionalNamespaces ...string) {
	triage.DumpIfFails(cs, triage.Config{
		ReportPath:           ReportPath,
		GroutMode:            true,
		InspectReporter:      inspectReporter,
		AdditionalNamespaces: additionalNamespaces,
		CollectFRRK8sPods:    true,
		CollectFRRContainers: true,
		CollectNodePCIInfo:   true,
	})
}
