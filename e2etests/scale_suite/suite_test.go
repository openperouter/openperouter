// SPDX-License-Identifier:Apache-2.0

package scale_e2e

import (
	"flag"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/scale_tests"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	updater *config.Updater
)

func handleFlags() {
	flag.StringVar(&executor.Kubectl, "kubectl", "kubectl", "the path for the kubectl binary")
	flag.Parse()
}

func TestMain(m *testing.M) {
	handleFlags()
	if testing.Short() {
		return
	}

	os.Exit(m.Run())
}

func TestScaleE2E(t *testing.T) {
	if testing.Short() {
		return
	}

	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E Scale Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	clientconfig, err := k8sclient.RestConfig()
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig (KUBECONFIG=%s)", os.Getenv("KUBECONFIG"))
	updater, err = config.UpdaterForCRs(clientconfig, openperouter.Namespace)
	Expect(err).NotTo(HaveOccurred())
	scale_tests.Updater = updater
})

var _ = ginkgo.AfterSuite(func() {
	if updater == nil {
		return
	}
	err := updater.CleanAll()
	Expect(err).NotTo(HaveOccurred())
})
