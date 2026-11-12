// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	admissionPollInterval = 100 * time.Millisecond
	admissionSteadyWindow = 15 * time.Second
	webhookRolloutTimeout = 3 * time.Minute
)

var _ = Describe("Webhook availability", GroutSupport, func() {
	var cs clientset.Interface

	vni := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "webhook-availability",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF:       "availability",
			VNI:       300,
			VXLanPort: new(int32(4789)),
		},
	}

	BeforeEach(func() {
		cs = k8sclient.New()
		Expect(Updater.CleanAll()).To(Succeed())
		Expect(Updater.Update(config.Resources{L3VNIs: []v1alpha1.L3VNI{vni}})).To(Succeed())
	})

	AfterEach(func() {
		dumpIfFails(cs)
		Expect(Updater.CleanAll()).To(Succeed())
	})

	It("admits every request while the webhook deployment rolls out", func() {
		deployment, err := openperouter.WebhookDeployment(cs)
		Expect(err).NotTo(HaveOccurred())

		By("restarting the webhook deployment")
		Expect(k8s.RestartDeployment(cs, deployment.Namespace, deployment.Name)).To(Succeed())

		By("updating a resource until the rollout completes")
		Eventually(func() error {
			if err := requestAdmission(&vni); err != nil {
				return StopTrying("admission failed during the rollout").Wrap(err)
			}
			return k8s.DeploymentRolledOut(cs, deployment.Namespace, deployment.Name)
		}).WithTimeout(webhookRolloutTimeout).WithPolling(admissionPollInterval).Should(Succeed())
	})

	// A replica that disappears without a graceful shutdown can fail the
	// requests it was serving, so only the recovery is asserted here.
	It("recovers after a webhook replica is killed", func() {
		deployment, err := openperouter.WebhookDeployment(cs)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for two ready replicas and any earlier rollout to finish")
		var podNames []string
		Eventually(func() ([]string, error) {
			podNames, err = readyWebhookPodNames(cs)
			return podNames, err
		}).WithTimeout(webhookRolloutTimeout).WithPolling(time.Second).Should(HaveLen(2))
		killedPodName := podNames[0]

		By("killing one webhook replica")
		Expect(cs.CoreV1().Pods(openperouter.Namespace).Delete(context.Background(), killedPodName,
			metav1.DeleteOptions{GracePeriodSeconds: new(int64(0))})).To(Succeed())

		By("waiting for ready replicas and admission recovery")
		Eventually(func() ([]string, error) {
			if err := k8s.DeploymentRolledOut(cs, deployment.Namespace, deployment.Name); err != nil {
				return nil, err
			}
			podNames, err := readyWebhookPodNames(cs)
			if err != nil {
				return nil, err
			}
			return podNames, requestAdmission(&vni)
		}).WithTimeout(webhookRolloutTimeout).WithPolling(time.Second).Should(
			And(HaveLen(2), Not(ContainElement(killedPodName))))

		By("checking that admission remains available")
		Consistently(func() error {
			return requestAdmission(&vni)
		}).WithTimeout(admissionSteadyWindow).WithPolling(admissionPollInterval).Should(Succeed())
	})

	DescribeTable("admits every request while another component restarts",
		func(labelSelector string) {
			pods, err := cs.CoreV1().Pods(openperouter.Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			Expect(err).To(Succeed())
			if len(pods.Items) == 0 {
				Skip(fmt.Sprintf("no pods matching %s in this deployment", labelSelector))
			}

			By("deleting the pods")
			oldPods, err := k8s.DeletePodsByLabel(cs, openperouter.Namespace, labelSelector)
			Expect(err).To(Succeed())

			By("updating a resource while the pods restart")
			observationStart := time.Now()
			Eventually(func() error {
				if err := requestAdmission(&vni); err != nil {
					return StopTrying("admission failed during the component restart").Wrap(err)
				}
				if err := k8s.PodsRolled(cs, openperouter.Namespace, labelSelector, oldPods); err != nil {
					return err
				}
				if time.Since(observationStart) < admissionSteadyWindow {
					return fmt.Errorf("minimum admission observation period of %s has not elapsed", admissionSteadyWindow)
				}
				return nil
			}).WithTimeout(webhookRolloutTimeout).WithPolling(admissionPollInterval).Should(Succeed())
		},
		Entry("node marker", openperouter.NodeMarkerLabelSelector),
		Entry("certificate controller", openperouter.CertControllerLabelSelector),
	)
})

// requestAdmission updates the resource so the API server calls the
// validating webhook. A patch is used because it cannot fail on a
// resourceVersion conflict with the controller's status updates.
func requestAdmission(vni *v1alpha1.L3VNI) error {
	patch := fmt.Sprintf(`{"metadata":{"annotations":{"openperouter.io/e2e-touch":%q}}}`,
		time.Now().Format(time.RFC3339Nano))
	return Updater.Client().Patch(context.Background(), vni.DeepCopy(),
		client.RawPatch(types.MergePatchType, []byte(patch)))
}

func readyWebhookPodNames(cs clientset.Interface) ([]string, error) {
	pods, err := k8s.PodsForLabel(cs, openperouter.Namespace, openperouter.WebhookLabelSelector)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(pods))
	for _, pod := range pods {
		if !k8s.PodIsReady(pod) {
			return nil, fmt.Errorf("webhook pod %q is not ready", pod.Name)
		}
		names = append(names, pod.Name)
	}
	return names, nil
}
