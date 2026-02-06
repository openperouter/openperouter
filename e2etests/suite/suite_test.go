// SPDX-License-Identifier:Apache-2.0

package e2e

import (
	"flag"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/tests"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	updater *config.Updater
)

// handleFlags sets up all flags and parses the command line.
func handleFlags() {
	flag.StringVar(&executor.Kubectl, "kubectl", "kubectl", "the path for the kubectl binary")
	flag.StringVar(&tests.ValidatorPath, "hostvalidator", "hostvalidator", "the path for the hostvalidator binary")
	flag.StringVar(&tests.ReportPath, "reporterpath", "/tmp", "the path for the reporter")
	flag.BoolVar(&tests.HostMode, "systemdmode", false, "tells if openperouter is running on the host")
	flag.BoolVar(&tests.SkipUnderlayPassthrough, "skip-underlay-passthrough", false, "skip creating underlay in passthrough tests")
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
	clientconfig := k8sclient.RestConfig()
	var err error
	updater, err = config.UpdaterForCRs(clientconfig, openperouter.Namespace)
	Expect(err).NotTo(HaveOccurred())
	tests.Updater = updater
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		ginkgo.Fail("KUBECONFIG not set")
	}
	tests.K8sReporter = k8s.InitReporter(kubeconfig, tests.ReportPath, openperouter.Namespace, frrk8s.Namespace)
})

var _ = ginkgo.AfterSuite(func() {
	err := updater.CleanAll()
	Expect(err).NotTo(HaveOccurred())
})
