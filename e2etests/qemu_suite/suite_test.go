// SPDX-License-Identifier:Apache-2.0

package qemu_e2e

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	nodeLinkConfigPath string
)

func handleFlags() {
	flag.StringVar(&executor.Kubectl, "kubectl", "kubectl", "the path for the kubectl binary")
	flag.StringVar(&ReportPath, "reporterpath", "/tmp", "the path for the reporter")
	flag.BoolVar(&HostMode, "systemdmode", false, "tells if openperouter is running on the host")
	flag.BoolVar(&GroutMode, "groutmode", false, "tells if openperouter is running with grout dataplane")
	flag.StringVar(&openperouter.Namespace, "openperouter-namespace", openperouter.Namespace, "namespace where OpenPERouter pods run")
	flag.StringVar(&nodeLinkConfigPath, "nodelink-config", "../nodelink-default.json", "path to node links config JSON")
	flag.Parse()
}

func TestMain(m *testing.M) {
	handleFlags()
	if testing.Short() {
		return
	}
	os.Exit(m.Run())
}

func TestQEMUE2E(t *testing.T) {
	if testing.Short() {
		return
	}
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "QEMU E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	log.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	clientconfig, err := k8sclient.RestConfig()
	Expect(err).NotTo(HaveOccurred(), "failed to load kubeconfig (KUBECONFIG=%s)", os.Getenv("KUBECONFIG"))
	Updater, err = config.UpdaterForCRs(clientconfig, openperouter.Namespace, frrk8s.Namespace)
	Expect(err).NotTo(HaveOccurred())

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	reporter, err := k8s.InitReporter(kubeconfig, ReportPath, openperouter.Namespace, "")
	Expect(err).NotTo(HaveOccurred(), "failed to initialize k8s reporter (kubeconfig=%s)", kubeconfig)
	k8sReporter = reporter

	ginkgo.By("Registering fabric and node links from " + nodeLinkConfigPath)
	Expect(infra.RegisterLinks(nodeLinkConfigPath)).To(Succeed())

	ginkgo.By("Waiting for router pods to be ready")
	cs := k8sclient.New()
	Expect(executor.SetupNodeExec(cs, frrk8s.Namespace, "busybox:1.36")).To(Succeed(), "failed to setup node-exec-helper")

	Eventually(func() error {
		routers, err := openperouter.Get(cs, HostMode)
		if err != nil {
			return err
		}
		return openperouter.AreReady(routers)
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
})

var _ = ginkgo.AfterSuite(func() {
	if Updater == nil {
		return
	}
	err := Updater.CleanAll()
	Expect(err).NotTo(HaveOccurred())
})
