// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gmeasure"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/metrics"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type scenarioUnderTest int

const (
	l2vni = iota
	l3vni
	l2l3vni
)

type scaleTestCase struct {
	hostMaster string
	scenario   scenarioUnderTest
	vniCount   int
}

func (tc scaleTestCase) String() string {
	if tc.scenario == l2l3vni {
		return fmt.Sprintf("%d pairs", tc.vniCount)
	}
	return fmt.Sprintf("%d VNIs", tc.vniCount)
}

const (
	routerLabelSelector     = "app=router"
	controllerLabelSelector = "app=controller"
)

var _ = Describe("VNI Scale Tests", Ordered, Label("scale"), func() {
	var experiment *gmeasure.Experiment

	BeforeAll(func() {
		experiment = gmeasure.NewExperiment("VNI Scale")
		AddReportEntry(experiment.Name, experiment)

		By("Verifying metrics-server is available")
		_, err := metrics.ForPod(executor.Kubectl, openperouter.Namespace, routerLabelSelector)
		Expect(err).NotTo(HaveOccurred(), "metrics-server must be running for scale tests")

		By("Cleaning up any existing VNI resources")
		Expect(Updater.CleanButUnderlay()).To(Succeed())

		By("Ensuring underlay configuration exists")
		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		By("Cleaning up all resources")
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("L2VNI only - Linux Bridge", func() {
		DescribeTable("VNI Scale Measurements",
			func(tc scaleTestCase) {
				runScaleTest(tc, experiment)
			},
			EntryDescription("%v"),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 1}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 50}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 150}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 200}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 250}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 300}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 350}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 400}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 450}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2vni, vniCount: 500}),
		)
	})

	Describe("L2VNI only - OVS Bridge", func() {
		DescribeTable("VNI Scale Measurements",
			func(tc scaleTestCase) {
				runScaleTest(tc, experiment)
			},
			EntryDescription("%v"),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 1}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 50}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 150}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 200}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 250}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 300}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 350}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 400}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 450}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2vni, vniCount: 500}),
		)
	})

	Describe("L3VNI+L2VNI - Linux Bridge", func() {
		DescribeTable("VNI Scale Measurements",
			func(tc scaleTestCase) {
				runScaleTest(tc, experiment)
			},
			EntryDescription("%v"),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 1}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 50}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 150}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 200}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 250}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 300}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 350}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 400}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 450}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.LinuxBridge, scenario: l2l3vni, vniCount: 500}),
		)
	})

	Describe("L3VNI+L2VNI - OVS Bridge", func() {
		DescribeTable("VNI Scale Measurements",
			func(tc scaleTestCase) {
				runScaleTest(tc, experiment)
			},
			EntryDescription("%v"),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 1}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 50}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 150}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 200}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 250}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 300}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 350}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 400}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 450}),
			Entry(nil, scaleTestCase{hostMaster: v1alpha1.OVSBridge, scenario: l2l3vni, vniCount: 500}),
		)
	})
})

func runScaleTest(tc scaleTestCase, experiment *gmeasure.Experiment) {
	testLabel := CurrentSpecReport().FullText()

	By("Waiting for VNI resources to be fully removed")
	waitForVNIsGone(Updater.Client())

	By("Creating " + testLabel)
	resources := buildResources(tc)

	experiment.MeasureDuration(testLabel+" reconcile", func() {
		err := Updater.Update(resources)
		Expect(err).NotTo(HaveOccurred())
	}, gmeasure.Annotation(fmt.Sprintf("%d VNIs", tc.vniCount)))

	By("Collecting stable memory metrics after reconcile")
	routerMem := collectStableMemory(routerLabelSelector)
	ctrlMem := collectStableMemory(controllerLabelSelector)

	experiment.RecordValue(testLabel+" router memory",
		routerMem.TotalMem,
		gmeasure.Units("MB"), gmeasure.Precision(2),
		gmeasure.Annotation(fmt.Sprintf("%d VNIs", tc.vniCount)),
	)
	experiment.RecordValue(testLabel+" controller memory",
		ctrlMem.TotalMem,
		gmeasure.Units("MB"), gmeasure.Precision(2),
		gmeasure.Annotation(fmt.Sprintf("%d VNIs", tc.vniCount)),
	)

	By("Cleaning up VNIs")
	err := Updater.CleanButUnderlay()
	Expect(err).NotTo(HaveOccurred())
}

func collectStableMemory(labelSelector string) metrics.MetricsSummaryResult {
	result, err := metrics.WaitForStableMemory(
		executor.Kubectl,
		openperouter.Namespace,
		labelSelector,
		metrics.DefaultMemoryConvergenceConfig(),
	)
	Expect(err).NotTo(HaveOccurred(), "metrics did not stabilize for %s", labelSelector)
	return result
}

func buildResources(tc scaleTestCase) config.Resources {
	switch tc.scenario {
	case l2l3vni:
		l3vnis, l2vnis := generateL3VNIsWithL2VNIs(tc.vniCount, openperouter.Namespace, tc.hostMaster)
		return config.Resources{
			L3VNIs: l3vnis,
			L2VNIs: l2vnis,
		}
	default:
		return config.Resources{
			L2VNIs: generateL2VNIs(tc.vniCount, openperouter.Namespace, tc.hostMaster),
		}
	}
}

func waitForVNIsGone(cli crclient.Client) {
	Eventually(func(g Gomega) int {
		l2list := &v1alpha1.L2VNIList{}
		g.Expect(cli.List(context.Background(), l2list, crclient.InNamespace(openperouter.Namespace))).To(Succeed())
		l3list := &v1alpha1.L3VNIList{}
		g.Expect(cli.List(context.Background(), l3list, crclient.InNamespace(openperouter.Namespace))).To(Succeed())
		return len(l2list.Items) + len(l3list.Items)
	}, 3*time.Minute, time.Second).Should(BeZero())
}

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
