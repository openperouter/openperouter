// SPDX-License-Identifier:Apache-2.0

package e2e

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/tests"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	updater            *config.Updater
	nodeLinkConfigPath string
	nodeExecImage      string
)

// handleFlags sets up all flags and parses the command line.
func handleFlags() {
	flag.StringVar(&executor.Kubectl, "kubectl", "kubectl", "the path for the kubectl binary")
	flag.StringVar(&tests.ReportPath, "reporterpath", "/tmp", "the path for the reporter")
	flag.BoolVar(&tests.HostMode, "systemdmode", false, "tells if openperouter is running on the host")
	flag.BoolVar(&tests.GroutMode, "groutmode", false, "tells if openperouter is running with grout dataplane")
	flag.BoolVar(&tests.SkipUnderlayPassthrough, "skip-underlay-passthrough", false, "skip creating underlay in passthrough tests")
	flag.StringVar(&frrk8s.Namespace, "frrk8s-namespace", frrk8s.Namespace, "namespace where FRR-K8s pods run")
	flag.StringVar(&openperouter.Namespace, "openperouter-namespace", openperouter.Namespace, "namespace where OpenPERouter pods run")
	flag.StringVar(&nodeLinkConfigPath, "nodelink-config", "../nodelink-default.json", "path to node links config JSON")
	flag.StringVar(&nodeExecImage, "node-exec-image", "busybox:1.36", "container image for node-exec-helper pods")
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

func TestE2E(t *testing.T) {
	if testing.Short() {
		return
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	clientconfig, err := k8sclient.RestConfig()
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig (KUBECONFIG=%s)", os.Getenv("KUBECONFIG"))
	updater, err = config.UpdaterForCRs(clientconfig, openperouter.Namespace, frrk8s.Namespace)
	Expect(err).NotTo(HaveOccurred())
	tests.Updater = updater
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	reporter, err := k8s.InitReporter(kubeconfig, tests.ReportPath, openperouter.Namespace, frrk8s.Namespace)
	Expect(err).NotTo(HaveOccurred(), "failed to initialize k8s reporter (kubeconfig=%s)", kubeconfig)
	tests.K8sReporter = reporter

	cs := k8sclient.New()
	Expect(executor.SetupNodeExec(cs, frrk8s.Namespace, nodeExecImage)).To(Succeed(), "failed to setup node-exec-helper")

	ginkgo.By("Registering fabric and node links from " + nodeLinkConfigPath)
	Expect(infra.RegisterLinks(nodeLinkConfigPath)).To(Succeed())

})

var _ = ginkgo.AfterSuite(func() {
	Expect(executor.TeardownNodeExec()).NotTo(HaveOccurred())

	if updater == nil {
		return
	}
	err := updater.CleanAll()
	Expect(err).NotTo(HaveOccurred())
})
