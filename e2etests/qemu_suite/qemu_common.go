// SPDX-License-Identifier:Apache-2.0

package qemu_e2e

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/triage"
	"github.com/openshift-kni/k8sreporter"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	Updater     *config.Updater
	HostMode    bool
	GroutMode   bool
	ReportPath  string
	k8sReporter *k8sreporter.KubernetesReporter
)

var GroutSupport = ginkgo.Label("grout-support")
var QEMUSupport = ginkgo.Label("qemu-support")

const Established = true

func dumpIfFails(cs clientset.Interface, additionalNamespaces ...string) {
	triage.DumpIfFails(cs, triage.Config{
		ReportPath:  ReportPath,
		HostMode:    HostMode,
		GroutMode:   GroutMode,
		K8sReporter: k8sReporter,
	}, additionalNamespaces...)
}
