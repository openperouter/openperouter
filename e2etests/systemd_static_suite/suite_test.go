// SPDX-License-Identifier:Apache-2.0

package systemd_static

import (
	"flag"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	updater       *config.Updater
	nodeExecImage string
)

// handleFlags sets up all flags and parses the command line.
func handleFlags() {
	flag.StringVar(&executor.Kubectl, "kubectl", "kubectl", "the path for the kubectl binary")
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

	cs := k8sclient.New()
	err := executor.SetupNodeExec(cs, frrk8s.Namespace, nodeExecImage)
	Expect(err).NotTo(HaveOccurred(), "failed to setup node-exec-helper")
})

var _ = ginkgo.AfterSuite(func() {
	Expect(executor.TeardownNodeExec()).NotTo(HaveOccurred())
})
