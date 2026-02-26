// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/metrics"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// scaleTestCase defines the parameters for a single scale test entry.
type scaleTestCase struct {
	hostMaster string // v1alpha1.LinuxBridge or v1alpha1.OVSBridge
	l2VNICount int
	l3VNICount int
}

const (
	routerLabelSelector     = "app=router"
	controllerLabelSelector = "app=controller"
	stabilizationDelay      = 10 * time.Second
	cleanupDelay            = 5 * time.Second
)

var _ = Describe("VNI Scale Tests", Ordered, Label("scale"), func() {
	var cs clientset.Interface
	var report *metrics.ScaleReport

	BeforeAll(func() {
		cs = k8sclient.New()
		report = metrics.NewScaleReport()

		By("Cleaning up any existing resources")
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("Setting up underlay configuration")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for router pods to be ready")
		waitForPodsReady(cs, openperouter.Namespace, routerLabelSelector)
		waitForPodsReady(cs, openperouter.Namespace, controllerLabelSelector)

		By("Collecting environment information")
		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())

		routerPods, err := k8s.PodsForLabel(cs, openperouter.Namespace, routerLabelSelector)
		Expect(err).NotTo(HaveOccurred())

		controllerPods, err := k8s.PodsForLabel(cs, openperouter.Namespace, controllerLabelSelector)
		Expect(err).NotTo(HaveOccurred())

		metricsAvailable := metrics.IsMetricsServerAvailable(executor.Kubectl)
		if !metricsAvailable {
			GinkgoWriter.Println("WARNING: metrics-server not available, CPU/memory values will be zero")
		}

		report.SetEnvironment(metrics.EnvironmentInfo{
			NodeCount:              len(nodes),
			RouterPodCount:         len(routerPods),
			ControllerPodCount:     len(controllerPods),
			MetricsServerAvailable: metricsAvailable,
		})
	})

	AfterAll(func() {
		By("Cleaning up all resources")
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		By("Printing scale test report to console")
		report.PrintConsole()

		By("Writing scale test report to JSON file")
		reportPath := filepath.Join(ReportPath, "scale-test-report.json")
		err = report.WriteJSON(reportPath)
		Expect(err).NotTo(HaveOccurred())
		GinkgoWriter.Printf("Scale test report written to: %s\n", reportPath)
	})

	DescribeTable("VNI Scale Measurements",
		func(tc scaleTestCase) {
			runScaleTest(cs, tc, report)
		},
		// L2VNI only tests - Linux Bridge
		Entry("L2VNI only: 1 VNI, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 1,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 50 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 50,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 150 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 150,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 200 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 200,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 250 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 250,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 300 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 300,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 350 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 350,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 400 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 400,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 450 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 450,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 500 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 500,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 550 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 550,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 600 VNIs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 600,
			l3VNICount: 0,
		}),
		// L2VNI only tests - OVS Bridge
		Entry("L2VNI only: 1 VNI, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 1,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 50 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 50,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 150 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 150,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 200 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 200,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 250 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 250,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 300 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 300,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 350 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 350,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 400 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 400,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 450 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 450,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 500 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 500,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 550 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 550,
			l3VNICount: 0,
		}),
		Entry("L2VNI only: 600 VNIs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 600,
			l3VNICount: 0,
		}),
		// L3VNI with L2VNI tests - Linux Bridge
		Entry("L3VNI+L2VNI: 1 pair, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 1,
			l3VNICount: 1,
		}),
		Entry("L3VNI+L2VNI: 50 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 50,
			l3VNICount: 50,
		}),
		Entry("L3VNI+L2VNI: 150 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 150,
			l3VNICount: 150,
		}),
		Entry("L3VNI+L2VNI: 200 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 200,
			l3VNICount: 200,
		}),
		Entry("L3VNI+L2VNI: 250 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 250,
			l3VNICount: 250,
		}),
		Entry("L3VNI+L2VNI: 300 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 300,
			l3VNICount: 300,
		}),
		Entry("L3VNI+L2VNI: 350 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 350,
			l3VNICount: 350,
		}),
		Entry("L3VNI+L2VNI: 400 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 400,
			l3VNICount: 400,
		}),
		Entry("L3VNI+L2VNI: 450 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 450,
			l3VNICount: 450,
		}),
		Entry("L3VNI+L2VNI: 500 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 500,
			l3VNICount: 500,
		}),
		Entry("L3VNI+L2VNI: 550 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 550,
			l3VNICount: 550,
		}),
		Entry("L3VNI+L2VNI: 600 pairs, linux-bridge", scaleTestCase{
			hostMaster: v1alpha1.LinuxBridge,
			l2VNICount: 600,
			l3VNICount: 600,
		}),
		// L3VNI with L2VNI tests - OVS Bridge
		Entry("L3VNI+L2VNI: 1 pair, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 1,
			l3VNICount: 1,
		}),
		Entry("L3VNI+L2VNI: 50 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 50,
			l3VNICount: 50,
		}),
		Entry("L3VNI+L2VNI: 150 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 150,
			l3VNICount: 150,
		}),
		Entry("L3VNI+L2VNI: 200 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 200,
			l3VNICount: 200,
		}),
		Entry("L3VNI+L2VNI: 250 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 250,
			l3VNICount: 250,
		}),
		Entry("L3VNI+L2VNI: 300 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 300,
			l3VNICount: 300,
		}),
		Entry("L3VNI+L2VNI: 350 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 350,
			l3VNICount: 350,
		}),
		Entry("L3VNI+L2VNI: 400 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 400,
			l3VNICount: 400,
		}),
		Entry("L3VNI+L2VNI: 450 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 450,
			l3VNICount: 450,
		}),
		Entry("L3VNI+L2VNI: 500 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 500,
			l3VNICount: 500,
		}),
		Entry("L3VNI+L2VNI: 550 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 550,
			l3VNICount: 550,
		}),
		Entry("L3VNI+L2VNI: 600 pairs, ovs-bridge", scaleTestCase{
			hostMaster: v1alpha1.OVSBridge,
			l2VNICount: 600,
			l3VNICount: 600,
		}),
	)
})

func runScaleTest(cs clientset.Interface, tc scaleTestCase, report *metrics.ScaleReport) {
	testDescription := describeScaleTest(tc)

	By(fmt.Sprintf("Collecting baseline metrics before %s", testDescription))
	baselineMetrics := collectAllMetrics()

	By(fmt.Sprintf("Creating %s", testDescription))
	startTime := time.Now()

	var resources config.Resources
	if tc.l3VNICount > 0 {
		l3vnis, l2vnis := generateL3VNIsWithL2VNIs(tc.l3VNICount, openperouter.Namespace, tc.hostMaster)
		resources = config.Resources{
			L3VNIs: l3vnis,
			L2VNIs: l2vnis,
		}
	} else {
		l2vnis := generateL2VNIs(tc.l2VNICount, openperouter.Namespace, tc.hostMaster)
		resources = config.Resources{
			L2VNIs: l2vnis,
		}
	}

	err := Updater.Update(resources)
	Expect(err).NotTo(HaveOccurred())

	By("Waiting for pods to stabilize")
	waitForPodsReady(cs, openperouter.Namespace, routerLabelSelector)
	waitForPodsReady(cs, openperouter.Namespace, controllerLabelSelector)
	time.Sleep(stabilizationDelay)

	By("Collecting scaled metrics")
	scaledMetrics := collectAllMetrics()
	scaledMetrics.VNICount = tc.l2VNICount
	if tc.l3VNICount > 0 {
		scaledMetrics.TestType = fmt.Sprintf("l3vni_with_l2vni_%s", tc.hostMaster)
	} else {
		scaledMetrics.TestType = fmt.Sprintf("l2vni_only_%s", tc.hostMaster)
	}
	scaledMetrics.Duration = time.Since(startTime)

	report.AddDataPoint(baselineMetrics, scaledMetrics)

	By("Cleaning up VNIs")
	err = Updater.CleanButUnderlay()
	Expect(err).NotTo(HaveOccurred())
	time.Sleep(cleanupDelay)
}

func describeScaleTest(tc scaleTestCase) string {
	if tc.l3VNICount > 0 {
		return fmt.Sprintf("%d L3VNI+L2VNI pairs (%s)", tc.l3VNICount, tc.hostMaster)
	}
	return fmt.Sprintf("%d L2VNIs (%s)", tc.l2VNICount, tc.hostMaster)
}

func collectAllMetrics() *metrics.ScaleMetrics {
	routerMetrics, err := metrics.CollectPodMetrics(
		executor.Kubectl,
		openperouter.Namespace,
		routerLabelSelector,
	)
	Expect(err).NotTo(HaveOccurred())

	controllerMetrics, err := metrics.CollectPodMetrics(
		executor.Kubectl,
		openperouter.Namespace,
		controllerLabelSelector,
	)
	Expect(err).NotTo(HaveOccurred())

	return &metrics.ScaleMetrics{
		RouterPodMetrics:     routerMetrics,
		ControllerPodMetrics: controllerMetrics,
		CollectionTime:       time.Now(),
	}
}

func waitForPodsReady(cs clientset.Interface, namespace, labelSelector string) {
	Eventually(func() error {
		pods, err := k8s.PodsForLabel(cs, namespace, labelSelector)
		if err != nil {
			return err
		}
		if len(pods) == 0 {
			return fmt.Errorf("no pods found with label %s", labelSelector)
		}
		for _, pod := range pods {
			ready := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					ready = true
					break
				}
			}
			if !ready {
				return fmt.Errorf("pod %s not ready", pod.Name)
			}
		}
		return nil
	}, 3*time.Minute, time.Second).ShouldNot(HaveOccurred())
}

// generateL2VNIs creates L2VNI resources for scale testing.
// VNI naming: l2vni-001 to l2vni-N
// VNI numbers: 1001 to 1000+N
// VRF names: vrf001 to vrfN (7 chars, within 15-char limit).
func generateL2VNIs(count int, namespace, bridgeType string) []v1alpha1.L2VNI {
	const baseVNI = 1000

	vnis := make([]v1alpha1.L2VNI, count)
	for i := 0; i < count; i++ {
		vrfName := fmt.Sprintf("vrf%03d", i+1)
		vnis[i] = v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("l2vni-%03d", i+1),
				Namespace: namespace,
			},
			Spec: v1alpha1.L2VNISpec{
				VRF:        ptr.To(vrfName),
				VNI:        uint32(baseVNI + i + 1),
				HostMaster: newHostMaster(bridgeType),
			},
		}
	}
	return vnis
}

// generateL3VNIsWithL2VNIs creates paired L3VNI and L2VNI resources.
// Each L2VNI gets its own L3VNI with the same VRF.
// L3VNI naming: l3vni-001 to l3vni-N, VNI numbers 2001-2000+N
// L2VNI naming: l2vni-001 to l2vni-N, VNI numbers 3001-3000+N.
func generateL3VNIsWithL2VNIs(count int, namespace, bridgeType string) ([]v1alpha1.L3VNI, []v1alpha1.L2VNI) {
	const baseL3VNI = 2000
	const baseL2VNI = 3000

	l3vnis := make([]v1alpha1.L3VNI, count)
	l2vnis := make([]v1alpha1.L2VNI, count)

	for i := 0; i < count; i++ {
		vrfName := fmt.Sprintf("vrf%03d", i+1)

		l3vnis[i] = v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("l3vni-%03d", i+1),
				Namespace: namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: vrfName,
				VNI: uint32(baseL3VNI + i + 1),
			},
		}

		l2vnis[i] = v1alpha1.L2VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("l2vni-%03d", i+1),
				Namespace: namespace,
			},
			Spec: v1alpha1.L2VNISpec{
				VRF:        ptr.To(vrfName),
				VNI:        uint32(baseL2VNI + i + 1),
				HostMaster: newHostMaster(bridgeType),
			},
		}
	}
	return l3vnis, l2vnis
}

// newHostMaster creates a HostMaster with AutoCreate enabled for the specified bridge type.
func newHostMaster(bridgeType string) *v1alpha1.HostMaster {
	switch bridgeType {
	case v1alpha1.LinuxBridge:
		return &v1alpha1.HostMaster{
			Type:        v1alpha1.LinuxBridge,
			LinuxBridge: &v1alpha1.LinuxBridgeConfig{AutoCreate: true},
		}
	case v1alpha1.OVSBridge:
		return &v1alpha1.HostMaster{
			Type:      v1alpha1.OVSBridge,
			OVSBridge: &v1alpha1.OVSBridgeConfig{AutoCreate: true},
		}
	default:
		return nil
	}
}
