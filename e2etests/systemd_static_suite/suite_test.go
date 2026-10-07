// SPDX-License-Identifier:Apache-2.0

package systemd_static

import (
	"flag"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/triage"
	"github.com/openshift-kni/k8sreporter"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	nodeExecImage string
	k8sReporter   *k8sreporter.KubernetesReporter
	reportPath    string
)

// handleFlags sets up all flags and parses the command line.
func handleFlags() {
	flag.StringVar(&executor.Kubectl, "kubectl", "kubectl", "the path for the kubectl binary")
	flag.StringVar(&nodeExecImage, "node-exec-image", "busybox:1.36", "container image for node-exec-helper pods")
	flag.StringVar(&reportPath, "reporterpath", "/tmp", "the path for the reporter")
	flag.Parse()
}

func TestMain(m *testing.M) {
	// Register test flags, then parse flags.
	handleFlags()
	if testing.Short() {
		return
	}

	os.Exit(m.Run())
}

func TestSystemdStatic(t *testing.T) {
	if testing.Short() {
		return
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Systemd Static Config Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		ginkgo.Fail("KUBECONFIG not set")
	}

	var err error
	k8sReporter, err = k8s.InitReporter(kubeconfig, reportPath, openperouter.Namespace, frrk8s.Namespace)
	Expect(err).NotTo(HaveOccurred(), "failed to initialize k8s reporter (kubeconfig=%s)", kubeconfig)
	Expect(executor.SetupNodeExec(k8sclient.New(), frrk8s.Namespace, nodeExecImage)).To(Succeed(), "failed to setup node-exec-helper")
})

var _ = ginkgo.AfterSuite(func() {
	Expect(executor.TeardownNodeExec()).NotTo(HaveOccurred())
})

func dumpIfFails(cs clientset.Interface, additionalNamespaces ...string) {
	triage.DumpIfFails(cs, triage.Config{
		ReportPath:           reportPath,
		K8sReporter:          k8sReporter,
		AdditionalNamespaces: additionalNamespaces,
		CollectFRRK8sPods:    false,
		CollectFRRContainers: true,
		IgnoreRouterPods:     true,
	})
}
