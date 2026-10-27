// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"fmt"
	"slices"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestParseChartResourceDefaults(t *testing.T) {
	g := NewGomegaWithT(t)
	specs := renderPodSpecs(g, operatorapi.OpenPERouterSpec{})

	want := map[string]v1.ResourceList{
		"frr":        {v1.ResourceCPU: resource.MustParse("250m"), v1.ResourceMemory: resource.MustParse("256Mi")},
		"reloader":   {v1.ResourceCPU: resource.MustParse("100m"), v1.ResourceMemory: resource.MustParse("128Mi")},
		"controller": {v1.ResourceCPU: resource.MustParse("250m"), v1.ResourceMemory: resource.MustParse("256Mi")},
		"nodemarker": {v1.ResourceCPU: resource.MustParse("100m"), v1.ResourceMemory: resource.MustParse("128Mi")},
	}
	containers := map[string]*v1.Container{
		"frr":        findContainer(specs[routerDaemonSetName], "frr"),
		"reloader":   findContainer(specs[routerDaemonSetName], "reloader"),
		"controller": findContainer(specs[controllerDaemonSetName], "controller"),
		"nodemarker": findContainer(specs[nodemarkerDeploymentName], "nodemarker"),
	}
	for name, container := range containers {
		g.Expect(container).ToNot(BeNil(), name)
		g.Expect(container.Resources.Requests).To(matchResourceQuantities(want[name]), "%s requests", name)
		g.Expect(container.Resources.Limits).To(BeEmpty(), "%s has limits by default", name)
	}
}

func TestParseChartPriorityClassName(t *testing.T) {
	g := NewGomegaWithT(t)
	specs := renderPodSpecs(g, operatorapi.OpenPERouterSpec{
		PriorityClassName: new("system-node-critical"),
	})

	for _, name := range []string{routerDaemonSetName, controllerDaemonSetName, nodemarkerDeploymentName} {
		g.Expect(specs).To(HaveKey(name))
		g.Expect(specs[name].PriorityClassName).To(Equal("system-node-critical"), name)
	}
}

func TestParseChartResourceOverrides(t *testing.T) {
	g := NewGomegaWithT(t)
	overridden := v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Limits: v1.ResourceList{v1.ResourceMemory: resource.MustParse("2Gi")},
	}
	specs := renderPodSpecs(g, operatorapi.OpenPERouterSpec{
		HealthProbePort: new(int32(9999)),
		Resources: &operatorapi.ContainerResources{
			Controller: &overridden,
			NodeMarker: &overridden,
			Reloader:   &overridden,
		},
	})

	overriddenContainers := map[string]*v1.Container{
		"controller": findContainer(specs[controllerDaemonSetName], "controller"),
		"nodemarker": findContainer(specs[nodemarkerDeploymentName], "nodemarker"),
		"reloader":   findContainer(specs[routerDaemonSetName], "reloader"),
	}
	for name, container := range overriddenContainers {
		g.Expect(container).ToNot(BeNil(), name)
		g.Expect(container.Resources.Requests).To(matchResourceQuantities(overridden.Requests), "%s requests", name)
		g.Expect(container.Resources.Limits).To(matchResourceQuantities(overridden.Limits), "%s limits", name)
	}

	frr := findContainer(specs[routerDaemonSetName], "frr")
	g.Expect(frr).ToNot(BeNil())
	g.Expect(frr.Resources.Requests).To(matchResourceQuantities(defaultFRRRequests()))

	controller := findContainer(specs[controllerDaemonSetName], "controller")
	g.Expect(controller.Args).To(ContainElement(ContainSubstring("9999")))
}

// renderPodSpecs renders the chart through the operator and returns pod specs
// keyed by workload name.
func renderPodSpecs(g Gomega, spec operatorapi.OpenPERouterSpec) map[string]v1.PodSpec {
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).To(Succeed())
	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{Name: "openperouter", Namespace: openperouterTestNamespace},
		Spec:       spec,
	}
	objects, err := chart.Objects(defaultEnvConfig, openperouter)
	g.Expect(err).To(Succeed())

	specs := map[string]v1.PodSpec{}
	for _, object := range objects {
		switch object.GetKind() {
		case daemonSetKind:
			daemonSet := appsv1.DaemonSet{}
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(object.UnstructuredContent(), &daemonSet)).To(Succeed())
			specs[object.GetName()] = daemonSet.Spec.Template.Spec
		case deploymentKind:
			deployment := appsv1.Deployment{}
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(object.UnstructuredContent(), &deployment)).To(Succeed())
			specs[object.GetName()] = deployment.Spec.Template.Spec
		}
	}
	g.Expect(specs).To(HaveKey(routerDaemonSetName))
	g.Expect(specs).To(HaveKey(controllerDaemonSetName))
	g.Expect(specs).To(HaveKey(nodemarkerDeploymentName))
	return specs
}

func findContainer(spec v1.PodSpec, name string) *v1.Container {
	for index := range spec.Containers {
		if spec.Containers[index].Name == name {
			return &spec.Containers[index]
		}
	}
	return nil
}

func defaultFRRRequests() v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("250m"),
		v1.ResourceMemory: resource.MustParse("256Mi"),
	}
}

func matchResourceQuantities(expected v1.ResourceList) types.GomegaMatcher {
	return WithTransform(resourceQuantities, ConsistOf(resourceQuantities(expected)))
}

func resourceQuantities(resources v1.ResourceList) []string {
	quantities := make([]string, 0, len(resources))
	for name, quantity := range resources {
		quantities = append(quantities, fmt.Sprintf("%s=%s", name, quantity.String()))
	}
	slices.Sort(quantities)
	return quantities
}
