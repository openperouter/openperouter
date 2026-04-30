/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tests

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
)

const skipWebhookLabel = "openperouter.io/skip-webhook"

var _ = Describe("Configuration Resiliency", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	goodL3VNI := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "good-l3",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "good",
			VNI: 100,
		},
	}

	goodL2VNI := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "good-l2",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: 200,
		},
	}

	conflictL3VNI := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "l3-conflict",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "conflict",
			VNI: 300,
		},
	}

	conflictL2VNI := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "l2-conflict",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L2VNISpec{
			VNI: 300,
		},
	}

	BeforeAll(func() {
		Expect(Updater.CleanAll()).To(Succeed())

		cs = k8sclient.New()
		var err error
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		disableWebhooksForNamespace(cs, openperouter.Namespace)

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
		})).To(Succeed())
	})

	AfterAll(func() {
		restoreWebhooks(cs, openperouter.Namespace)

		Expect(Updater.CleanAll()).To(Succeed())

		Eventually(func() error {
			newRouters, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.DaemonsetRolled(routers, newRouters)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		Expect(Updater.CleanButUnderlay()).To(Succeed())
	})

	Context("when L3VNI and L2VNI have the same VNI number", func() {
		It("should quarantine the conflicting L2VNI and configure the good resources", func() {
			Expect(Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{goodL3VNI, conflictL3VNI},
				L2VNIs: []v1alpha1.L2VNI{goodL2VNI, conflictL2VNI},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				status := getNodeStatus(g, infra.KindControlPlane)
				g.Expect(status.Status.FailedResources).To(HaveLen(1))
				g.Expect(status.Status.FailedResources[0].Kind).To(Equal("L2VNI"))
				g.Expect(status.Status.FailedResources[0].Name).To(Equal("l2-conflict"))
				g.Expect(status.Status.FailedResources[0].Reason).To(Equal(v1alpha1.ValidationFailed))
				g.Expect(status.Status.FailedResources[0].Message).To(ContainSubstring("duplicate VNI"))

				readyCond := apimeta.FindStatusCondition(status.Status.Conditions, "Ready")
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))

				degradedCond := apimeta.FindStatusCondition(status.Status.Conditions, "Degraded")
				g.Expect(degradedCond).NotTo(BeNil())
				g.Expect(degradedCond.Status).To(Equal(metav1.ConditionTrue))
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("when an L3VNI has an invalid route target", func() {
		It("should quarantine the L3VNI and cascade DependencyFailed to its L2VNIs", func() {
			badRTL3VNI := v1alpha1.L3VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bad-rt-l3",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L3VNISpec{
					VRF:       "cascade",
					VNI:       400,
					ExportRTs: []string{"invalid-rt"},
				},
			}

			cascadeL2VNI := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cascade-l2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L2VNISpec{
					VRF: ptr.To("cascade"),
					VNI: 401,
				},
			}

			Expect(Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{goodL3VNI, badRTL3VNI},
				L2VNIs: []v1alpha1.L2VNI{goodL2VNI, cascadeL2VNI},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				status := getNodeStatus(g, infra.KindControlPlane)
				g.Expect(status.Status.FailedResources).To(HaveLen(2))

				failedByName := map[string]v1alpha1.FailedResource{}
				for _, f := range status.Status.FailedResources {
					failedByName[f.Name] = f
				}

				g.Expect(failedByName).To(HaveKey("bad-rt-l3"))
				g.Expect(failedByName["bad-rt-l3"].Kind).To(Equal("L3VNI"))
				g.Expect(failedByName["bad-rt-l3"].Reason).To(Equal(v1alpha1.ValidationFailed))

				g.Expect(failedByName).To(HaveKey("cascade-l2"))
				g.Expect(failedByName["cascade-l2"].Kind).To(Equal("L2VNI"))
				g.Expect(failedByName["cascade-l2"].Reason).To(Equal(v1alpha1.DependencyFailed))
				g.Expect(failedByName["cascade-l2"].Message).To(ContainSubstring("quarantined"))

				readyCond := apimeta.FindStatusCondition(status.Status.Conditions, "Ready")
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("when an L2VNI references a VRF with no matching L3VNI", func() {
		It("should report DependencyFailed for the orphan L2VNI", func() {
			orphanL2VNI := v1alpha1.L2VNI{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orphan-l2",
					Namespace: openperouter.Namespace,
				},
				Spec: v1alpha1.L2VNISpec{
					VRF: ptr.To("nonexistent"),
					VNI: 500,
				},
			}

			Expect(Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{goodL3VNI},
				L2VNIs: []v1alpha1.L2VNI{goodL2VNI, orphanL2VNI},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				status := getNodeStatus(g, infra.KindControlPlane)
				g.Expect(status.Status.FailedResources).To(HaveLen(1))
				g.Expect(status.Status.FailedResources[0].Kind).To(Equal("L2VNI"))
				g.Expect(status.Status.FailedResources[0].Name).To(Equal("orphan-l2"))
				g.Expect(status.Status.FailedResources[0].Reason).To(Equal(v1alpha1.DependencyFailed))
				g.Expect(status.Status.FailedResources[0].Message).To(ContainSubstring("no L3VNI found"))

				readyCond := apimeta.FindStatusCondition(status.Status.Conditions, "Ready")
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("when a cross-type VNI conflict is resolved", func() {
		It("should recover and clear the status", func() {
			By("creating a cross-type VNI conflict")
			Expect(Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{conflictL3VNI},
				L2VNIs: []v1alpha1.L2VNI{conflictL2VNI},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				status := getNodeStatus(g, infra.KindControlPlane)
				g.Expect(status.Status.FailedResources).To(HaveLen(1))
				g.Expect(status.Status.FailedResources[0].Name).To(Equal("l2-conflict"))
			}, time.Minute, time.Second).Should(Succeed())

			By("fixing the L2VNI to use a non-conflicting VNI number")
			fixedL2VNI := conflictL2VNI.DeepCopy()
			fixedL2VNI.Spec.VNI = 301

			Expect(Updater.Update(config.Resources{
				L3VNIs: []v1alpha1.L3VNI{conflictL3VNI},
				L2VNIs: []v1alpha1.L2VNI{*fixedL2VNI},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				status := getNodeStatus(g, infra.KindControlPlane)
				g.Expect(status.Status.FailedResources).To(BeEmpty())

				readyCond := apimeta.FindStatusCondition(status.Status.Conditions, "Ready")
				g.Expect(readyCond).NotTo(BeNil())
				g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))

				degradedCond := apimeta.FindStatusCondition(status.Status.Conditions, "Degraded")
				g.Expect(degradedCond).NotTo(BeNil())
				g.Expect(degradedCond.Status).To(Equal(metav1.ConditionFalse))
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})

func getNodeStatus(g Gomega, nodeName string) *v1alpha1.RouterNodeConfigurationStatus {
	status := &v1alpha1.RouterNodeConfigurationStatus{}
	err := Updater.Client().Get(context.Background(), client.ObjectKey{
		Name:      nodeName,
		Namespace: openperouter.Namespace,
	}, status)
	g.Expect(err).NotTo(HaveOccurred())
	return status
}

func findOpenPEWebhookConfigurations(cs clientset.Interface) []string {
	vwcList, err := cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(
		context.Background(), metav1.ListOptions{})
	ExpectWithOffset(2, err).NotTo(HaveOccurred())

	var names []string
	for _, vwc := range vwcList.Items {
		for _, wh := range vwc.Webhooks {
			if strings.Contains(wh.Name, "openperouter") {
				names = append(names, vwc.Name)
				break
			}
		}
	}
	ExpectWithOffset(2, names).NotTo(BeEmpty(), "no ValidatingWebhookConfiguration found with openperouter webhooks")
	return names
}

func disableWebhooksForNamespace(cs clientset.Interface, namespace string) {
	By("disabling webhooks for namespace " + namespace)

	_, err := cs.CoreV1().Namespaces().Patch(
		context.Background(),
		namespace,
		types.MergePatchType,
		[]byte(`{"metadata":{"labels":{"`+skipWebhookLabel+`":""}}}`),
		metav1.PatchOptions{},
	)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	for _, name := range findOpenPEWebhookConfigurations(cs) {
		vwc, err := cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
			context.Background(), name, metav1.GetOptions{})
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		for i := range vwc.Webhooks {
			vwc.Webhooks[i].NamespaceSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      skipWebhookLabel,
						Operator: metav1.LabelSelectorOpDoesNotExist,
					},
				},
			}
		}

		_, err = cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(
			context.Background(), vwc, metav1.UpdateOptions{})
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	}
}

func restoreWebhooks(cs clientset.Interface, namespace string) {
	By("restoring webhooks for namespace " + namespace)

	labelPatch, _ := json.Marshal([]map[string]interface{}{
		{
			"op":   "remove",
			"path": "/metadata/labels/" + jsonPatchEscape(skipWebhookLabel),
		},
	})
	_, err := cs.CoreV1().Namespaces().Patch(
		context.Background(),
		namespace,
		types.JSONPatchType,
		labelPatch,
		metav1.PatchOptions{},
	)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	for _, name := range findOpenPEWebhookConfigurations(cs) {
		vwc, err := cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
			context.Background(), name, metav1.GetOptions{})
		ExpectWithOffset(1, err).NotTo(HaveOccurred())

		for i := range vwc.Webhooks {
			vwc.Webhooks[i].NamespaceSelector = nil
		}

		_, err = cs.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(
			context.Background(), vwc, metav1.UpdateOptions{})
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
	}
}

func jsonPatchEscape(s string) string {
	replacer := strings.NewReplacer("~", "~0", "/", "~1")
	return replacer.Replace(s)
}
