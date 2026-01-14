/*
Copyright 2024.

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

package operator

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
)

var _ = Describe("OpenPERouter Controller", func() {
	Context("When reconciling a resource", func() {
		BeforeEach(func() {})

		AfterEach(func() {
			err := cleanTestNamespace()
			Expect(err).ToNot(HaveOccurred())
		})
		controllerImage := "quay.io/openperouter/router:test"
		It("should successfully reconcile the resource", func() {
			controllerContainers := map[string]string{
				"controller": controllerImage,
			}
			routerContainers := map[string]string{
				"frr":      controllerImage,
				"reloader": controllerImage,
			}
			routerInitContainers := map[string]string{
				"cp-frr-files": controllerImage,
			}
			nodemarkerContainers := map[string]string{
				"nodemarker": controllerImage,
			}

			openperouter := &operatorapi.OpenPERouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openperouter",
					Namespace: openperouterTestNamespace,
				},
				Spec: operatorapi.OpenPERouterSpec{
					LogLevel: "debug",
				},
			}

			By("Creating the OpenPERouter resource")
			err := k8sClient.Create(context.Background(), openperouter)
			Expect(err).ToNot(HaveOccurred())

			By("Validating that the variables were templated correctly")
			controllerDaemonSet := &appsv1.DaemonSet{}
			Eventually(func() error {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: controllerDaemonSetName, Namespace: openperouterTestNamespace}, controllerDaemonSet)
				return err
			}, 2*time.Second, 200*time.Millisecond).ShouldNot((HaveOccurred()))
			Expect(controllerDaemonSet).NotTo(BeZero())
			Expect(controllerDaemonSet.Spec.Template.Spec.Containers).To(HaveLen(len(controllerContainers)))
			Expect(controllerDaemonSet.OwnerReferences).ToNot(BeNil())
			Expect(controllerDaemonSet.OwnerReferences[0].Kind).To(Equal("OpenPERouter"))
			for _, c := range controllerDaemonSet.Spec.Template.Spec.Containers {
				image, ok := controllerContainers[c.Name]
				Expect(ok).To(BeTrue(), fmt.Sprintf("container %s not found in %s", c.Name, controllerContainers))
				Expect(c.Image).To(Equal(image), fmt.Sprintf("container %s uses wrong image", c.Name))
			}
			Expect(validateLogLevel("debug", controllerDaemonSet.Spec.Template)).NotTo(HaveOccurred())

			routerDaemonSet := &appsv1.DaemonSet{}
			Eventually(func() error {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routerDaemonSetName, Namespace: openperouterTestNamespace}, routerDaemonSet)
				return err
			}, 2*time.Second, 200*time.Millisecond).ShouldNot((HaveOccurred()))
			Expect(routerDaemonSet).NotTo(BeZero())
			Expect(routerDaemonSet.Spec.Template.Spec.Containers).To(HaveLen(len(routerContainers)))
			Expect(routerDaemonSet.OwnerReferences).ToNot(BeNil())
			Expect(routerDaemonSet.OwnerReferences[0].Kind).To(Equal("OpenPERouter"))
			for _, c := range routerDaemonSet.Spec.Template.Spec.Containers {
				image, ok := routerContainers[c.Name]
				Expect(ok).To(BeTrue(), fmt.Sprintf("container %s not found in %s", c.Name, routerContainers))
				Expect(c.Image).To(Equal(image), fmt.Sprintf("container %s uses wrong image", c.Name))
			}
			for _, c := range routerDaemonSet.Spec.Template.Spec.InitContainers {
				image, ok := routerInitContainers[c.Name]
				Expect(ok).To(BeTrue(), fmt.Sprintf("init container %s not found in %s", c.Name, routerInitContainers))
				Expect(c.Image).To(Equal(image), fmt.Sprintf("init container %s uses wrong image", c.Name))
			}
			Expect(validateLogLevel("debug", routerDaemonSet.Spec.Template)).NotTo(HaveOccurred())

			nodemarkerDeployment := &appsv1.Deployment{}
			Eventually(func() error {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: nodemarkerDeploymentName, Namespace: openperouterTestNamespace}, nodemarkerDeployment)
				return err
			}, 2*time.Second, 200*time.Millisecond).ShouldNot((HaveOccurred()))
			Expect(nodemarkerDeployment).NotTo(BeZero())
			Expect(nodemarkerDeployment.Spec.Template.Spec.Containers).To(HaveLen(len(nodemarkerContainers)))
			Expect(nodemarkerDeployment.OwnerReferences).ToNot(BeNil())
			Expect(nodemarkerDeployment.OwnerReferences[0].Kind).To(Equal("OpenPERouter"))
			for _, c := range nodemarkerDeployment.Spec.Template.Spec.Containers {
				image, ok := nodemarkerContainers[c.Name]
				Expect(ok).To(BeTrue(), fmt.Sprintf("container %s not found in %s", c.Name, nodemarkerContainers))
				Expect(c.Image).To(Equal(image), fmt.Sprintf("container %s uses wrong image", c.Name))
			}
			Expect(validateLogLevel("debug", nodemarkerDeployment.Spec.Template)).NotTo(HaveOccurred())

			By("Updating the OpenPERouter resource")
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: "openperouter", Namespace: openperouterTestNamespace}, openperouter)
			Expect(err).ToNot(HaveOccurred())
			openperouter.Spec.LogLevel = "info"
			err = k8sClient.Update(context.Background(), openperouter)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() error {
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: controllerDaemonSetName, Namespace: openperouterTestNamespace}, controllerDaemonSet)
				if err != nil {
					return err
				}
				err = validateLogLevel("info", controllerDaemonSet.Spec.Template)
				if err != nil {
					return err
				}

				err = k8sClient.Get(context.Background(), types.NamespacedName{Name: routerDaemonSetName, Namespace: openperouterTestNamespace}, routerDaemonSet)
				if err != nil {
					return err
				}
				err = validateLogLevel("info", routerDaemonSet.Spec.Template)
				if err != nil {
					return err
				}

				err = k8sClient.Get(context.Background(), types.NamespacedName{Name: nodemarkerDeploymentName, Namespace: openperouterTestNamespace}, nodemarkerDeployment)
				if err != nil {
					return err
				}
				err = validateLogLevel("info", nodemarkerDeployment.Spec.Template)
				if err != nil {
					return err
				}

				return nil
			}, 2*time.Second, 200*time.Millisecond).ShouldNot((HaveOccurred()))
		})
	})
})

func cleanTestNamespace() error {
	err := k8sClient.DeleteAllOf(context.Background(), &operatorapi.OpenPERouter{}, client.InNamespace(openperouterTestNamespace))
	if err != nil {
		return err
	}
	err = k8sClient.DeleteAllOf(context.Background(), &appsv1.Deployment{}, client.InNamespace(openperouterTestNamespace))
	if err != nil {
		return err
	}
	err = k8sClient.DeleteAllOf(context.Background(), &appsv1.DaemonSet{}, client.InNamespace(openperouterTestNamespace))
	return err
}

func validateLogLevel(level string, pod v1.PodTemplateSpec) error {
	foundOne := false
	for _, c := range pod.Spec.Containers {
		for _, arg := range c.Args {
			if !strings.Contains(arg, "--loglevel") {
				continue
			}
			if arg == fmt.Sprintf("--loglevel=%s", level) {
				foundOne = true
				continue
			}
			return fmt.Errorf("got incorrect loglevel: %s, expected %s", arg, level)
		}
	}
	if !foundOne {
		return fmt.Errorf("pod %v has no loglevel arg", pod)
	}
	return nil
}
