// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/triage"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

var (
	Updater                 *config.Updater
	InspectReporter         *k8s.InspectReporter
	ReportPath              string
	HostMode                bool
	GroutMode               bool
	SkipUnderlayPassthrough bool
)

var GroutSupport = ginkgo.Label("grout-support")

// GroutOnly marks specs that must run exclusively on the grout lanes. The grout
// lanes select them via the label filter and the non-grout lanes exclude them.
var GroutOnly = ginkgo.Label("grout-only")

// PodModeOnly is the opposite of the "systemdmode" label: that one marks specs
// that run only under systemd and is excluded by the pod lanes, this one marks
// specs that run only when openperouter runs in a pod, such as those reaching
// into the router pod's containers, and is excluded by the systemd lane.
var PodModeOnly = ginkgo.Label("podmode-only")

func dumpIfFails(cs clientset.Interface, additionalNamespaces ...string) {
	triage.DumpIfFails(cs, triage.Config{
		ReportPath:           ReportPath,
		HostMode:             HostMode,
		GroutMode:            GroutMode,
		InspectReporter:      InspectReporter,
		AdditionalNamespaces: additionalNamespaces,
		CollectFRRK8sPods:    true,
		CollectFRRContainers: true,
	})
}

func DumpPods(name string, pods []*corev1.Pod) {
	triage.DumpPods(name, pods)
}
