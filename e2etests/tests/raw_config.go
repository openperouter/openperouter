// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var _ = Describe("RawFRRConfig", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	BeforeAll(func() {
		cs = k8sclient.New()
		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		err = Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	BeforeEach(func() {
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())
	})

	It("should append a raw config snippet to the FRR configuration", func() {
		rawConfig := v1alpha1.RawFRRConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-raw-config",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.RawFRRConfigSpec{
				RawConfig: "ip prefix-list raw-test-list seq 10 permit 10.111.0.0/16",
			},
		}

		err := Updater.Update(config.Resources{
			RawFRRConfigs: []v1alpha1.RawFRRConfig{rawConfig},
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return checkRawConfigOnAllRouters(routers, "ip prefix-list raw-test-list seq 10 permit 10.111.0.0/16")
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	It("should order multiple raw config snippets by priority", func() {
		rawConfigHigh := v1alpha1.RawFRRConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "raw-high-priority",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.RawFRRConfigSpec{
				Priority:  ptr.To(int32(20)),
				RawConfig: "ip prefix-list raw-high seq 10 permit 10.222.0.0/16",
			},
		}

		rawConfigLow := v1alpha1.RawFRRConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "raw-low-priority",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.RawFRRConfigSpec{
				Priority:  ptr.To(int32(5)),
				RawConfig: "ip prefix-list raw-low seq 10 permit 10.33.0.0/16",
			},
		}

		err := Updater.Update(config.Resources{
			RawFRRConfigs: []v1alpha1.RawFRRConfig{rawConfigHigh, rawConfigLow},
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			return checkRawConfigOrderOnAllRouters(routers,
				"ip prefix-list raw-low seq 10 permit 10.33.0.0/16",
				"ip prefix-list raw-high seq 10 permit 10.222.0.0/16")
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	It("should apply raw config only to nodes matching the node selector", func() {
		nodeSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"openperouter/rawconfig-test": "true",
			},
		}

		nodes, err := k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodes)).To(BeNumerically(">=", 2), "Expected at least 2 nodes, but got fewer")

		targetNode := nodes[0]
		By(fmt.Sprintf("Labeling node %s for raw config node selector test", targetNode.Name))
		Expect(
			k8s.LabelNodes(cs, nodeSelector.MatchLabels, targetNode),
		).To(Succeed())

		DeferCleanup(func() {
			k8s.UnlabelNodes(cs, targetNode)
		})

		rawConfig := v1alpha1.RawFRRConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "raw-node-selector",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.RawFRRConfigSpec{
				NodeSelector: nodeSelector,
				RawConfig:    "ip prefix-list raw-node-specific seq 10 permit 10.44.0.0/16",
			},
		}

		err = Updater.Update(config.Resources{
			RawFRRConfigs: []v1alpha1.RawFRRConfig{rawConfig},
		})
		Expect(err).NotTo(HaveOccurred())

		expected := "ip prefix-list raw-node-specific seq 10 permit 10.44.0.0/16"

		Eventually(func() error {
			for router := range routers.GetExecutors() {
				runningConfig, err := frr.RunningConfig(router)
				if err != nil {
					return err
				}
				hasConfig := strings.Contains(runningConfig, expected)
				isTarget := strings.Contains(router.Name(), targetNode.Name)

				if isTarget && !hasConfig {
					return fmt.Errorf("target router %s running config does not contain %q", router.Name(), expected)
				}
				if !isTarget && hasConfig {
					return fmt.Errorf("non-target router %s running config unexpectedly contains %q", router.Name(), expected)
				}
			}
			return nil
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	It("should remove raw config when the resource is deleted", func() {
		rawConfig := v1alpha1.RawFRRConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "raw-delete-test",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.RawFRRConfigSpec{
				RawConfig: "ip prefix-list raw-delete-me seq 10 permit 10.55.0.0/16",
			},
		}

		err := Updater.Update(config.Resources{
			RawFRRConfigs: []v1alpha1.RawFRRConfig{rawConfig},
		})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the raw config to appear")
		Eventually(func() error {
			return checkRawConfigOnAllRouters(routers, "ip prefix-list raw-delete-me seq 10 permit 10.55.0.0/16")
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		By("cleaning the raw config resources")
		err = Updater.CleanButUnderlay()
		Expect(err).NotTo(HaveOccurred())

		By("waiting for the raw config to be removed")
		Eventually(func() error {
			return checkRawConfigAbsentOnAllRouters(routers, "ip prefix-list raw-delete-me seq 10 permit 10.55.0.0/16")
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})
})

func checkRawConfigOnAllRouters(routers openperouter.Routers, expected string) error {
	for router := range routers.GetExecutors() {
		if err := checkRouterHasConfig(router, expected); err != nil {
			return err
		}
	}
	return nil
}

func checkRouterHasConfig(router openperouter.RouterExecutor, expected string) error {
	output, err := frr.RunningConfig(router)
	if err != nil {
		return err
	}
	if !strings.Contains(output, expected) {
		return fmt.Errorf("router %s running config does not contain %q", router.Name(), expected)
	}
	return nil
}

func checkRawConfigAbsentOnAllRouters(routers openperouter.Routers, unexpected string) error {
	for router := range routers.GetExecutors() {
		output, err := frr.RunningConfig(router)
		if err != nil {
			return err
		}
		if strings.Contains(output, unexpected) {
			return fmt.Errorf("router %s running config still contains %q", router.Name(), unexpected)
		}
	}
	return nil
}

func checkRawConfigOrderOnAllRouters(routers openperouter.Routers, first, second string) error {
	for router := range routers.GetExecutors() {
		output, err := frr.RunningConfig(router)
		if err != nil {
			return err
		}
		firstIdx := strings.Index(output, first)
		secondIdx := strings.Index(output, second)
		if firstIdx == -1 {
			return fmt.Errorf("router %s running config does not contain %q", router.Name(), first)
		}
		if secondIdx == -1 {
			return fmt.Errorf("router %s running config does not contain %q", router.Name(), second)
		}
		if firstIdx >= secondIdx {
			return fmt.Errorf("router %s: expected %q (priority lower) before %q (priority higher), but found in wrong order",
				router.Name(), first, second)
		}
	}
	return nil
}
