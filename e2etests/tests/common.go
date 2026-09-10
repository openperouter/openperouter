// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/triage"
	"github.com/openshift-kni/k8sreporter"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	Updater                 *config.Updater
	K8sReporter             *k8sreporter.KubernetesReporter
	ReportPath              string
	HostMode                bool
	GroutMode               bool
	SkipUnderlayPassthrough bool
)

var GroutSupport = ginkgo.Label("grout-support")

// GroutOnly marks specs that must run exclusively on the grout lanes. The grout
// lanes select them via the label filter and the non-grout lanes exclude them.
var GroutOnly = ginkgo.Label("grout-only")

func dumpIfFails(cs clientset.Interface, additionalNamespaces ...string) {
	triage.DumpIfFails(cs, triage.Config{
		ReportPath:           ReportPath,
		HostMode:             HostMode,
		GroutMode:            GroutMode,
		K8sReporter:          K8sReporter,
		AdditionalNamespaces: additionalNamespaces,
		CollectFRRK8sPods:    true,
		CollectFRRContainers: true,
	})
}

func DumpPods(name string, pods []*corev1.Pod) {
	triage.DumpPods(name, pods)
}
