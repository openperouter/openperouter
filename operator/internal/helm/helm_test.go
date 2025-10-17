/*


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

package helm

import (
	"fmt"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	"github.com/openperouter/openperouter/operator/internal/envconfig"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// var update = flag.Bool("update", false, "update .golden files")

const (
	invalidChartPath          = "../../bindata/deployment/no-chart"
	testChartPath             = "../../bindata/deployment/openperouter"
	openperouterChartName     = "openperouter"
	openperouterTestNamespace = "openperouter-test-namespace"
	controllerDaemonSetName   = "controller"
	routerDaemonSetName       = "router"
	nodemarkerDeploymentName  = "nodemarker"
	daemonSetKind             = "DaemonSet"
	deploymentKind            = "Deployment"
)

var defaultEnvConfig = envconfig.EnvConfig{
	ControllerImage: envconfig.ImageInfo{
		Repo: "quay.io/openperouter/router",
		Tag:  "test",
	},
	FRRImage: envconfig.ImageInfo{
		Repo: "quay.io/frrouting/frr",
		Tag:  "test",
	},
	MetricsPort:    7472,
	FRRMetricsPort: 7473,
	Namespace:      openperouterTestNamespace,
}

func TestLoadChart(t *testing.T) {
	g := NewGomegaWithT(t)
	_, err := NewChart(invalidChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).To(HaveOccurred())
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(chart.chart).ToNot(BeNil())
	g.Expect(chart.chart.Name()).To(Equal(openperouterChartName))
}

func TestParseChartWithCustomValues(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())
	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openperouter",
			Namespace: openperouterTestNamespace,
		},
		Spec: operatorapi.OpenPERouterSpec{
			LogLevel: "info",
		},
	}

	objs, err := chart.Objects(defaultEnvConfig, openperouter)
	g.Expect(err).ToNot(HaveOccurred())

	validateController := func(ds appsv1.DaemonSet) error {
		err = validateLogLevel("info", ds.Spec.Template)
		return err
	}

	validateRouter := func(ds appsv1.DaemonSet) error {
		err = validateLogLevel("info", ds.Spec.Template)
		return err
	}

	validateNodemarker := func(d appsv1.Deployment) error {
		err = validateLogLevel("info", d.Spec.Template)
		return err
	}

	var routerFound, controllerFound, nodemarkerFound bool
	for _, obj := range objs {
		objKind := obj.GetKind()
		if objKind == daemonSetKind && obj.GetName() == controllerDaemonSetName {
			controller := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &controller)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(controller.GetName()).To(Equal(controllerDaemonSetName))
			g.Expect(validateController(controller)).ToNot(HaveOccurred())
			controllerFound = true
		}
		if objKind == daemonSetKind && obj.GetName() == routerDaemonSetName {
			router := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &router)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(router.GetName()).To(Equal(routerDaemonSetName))
			g.Expect(validateRouter(router)).ToNot(HaveOccurred())
			routerFound = true
		}
		if objKind == deploymentKind && obj.GetName() == nodemarkerDeploymentName {
			g.Expect(obj.GetName()).To(Equal(nodemarkerDeploymentName))
			nodemarker := appsv1.Deployment{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &nodemarker)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(nodemarker.GetName()).To(Equal(nodemarkerDeploymentName))
			g.Expect(validateNodemarker(nodemarker)).ToNot(HaveOccurred())
			nodemarkerFound = true
		}
	}
	g.Expect(controllerFound).To(BeTrue())
	g.Expect(routerFound).To(BeTrue())
	g.Expect(nodemarkerFound).To(BeTrue())
}

func TestParseChartWithMultusAnnotation(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	multusAnnotation := "macvlan-conf"
	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openperouter",
			Namespace: openperouterTestNamespace,
		},
		Spec: operatorapi.OpenPERouterSpec{
			LogLevel:                "info",
			MultusNetworkAnnotation: multusAnnotation,
		},
	}

	objs, err := chart.Objects(defaultEnvConfig, openperouter)
	g.Expect(err).ToNot(HaveOccurred())

	var routerFound bool
	for _, obj := range objs {
		objKind := obj.GetKind()
		if objKind == daemonSetKind && obj.GetName() == routerDaemonSetName {
			router := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &router)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(router.GetName()).To(Equal(routerDaemonSetName))

			// Verify the multus annotation is present in the router pod template
			annotations := router.Spec.Template.Annotations
			g.Expect(annotations).ToNot(BeNil())
			g.Expect(annotations["k8s.v1.cni.cncf.io/networks"]).To(Equal(multusAnnotation))
			routerFound = true
		}
	}
	g.Expect(routerFound).To(BeTrue())
}

func TestParseChartWithoutMultusAnnotation(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openperouter",
			Namespace: openperouterTestNamespace,
		},
		Spec: operatorapi.OpenPERouterSpec{
			LogLevel: "info",
		},
	}

	objs, err := chart.Objects(defaultEnvConfig, openperouter)
	g.Expect(err).ToNot(HaveOccurred())

	var routerFound bool
	for _, obj := range objs {
		objKind := obj.GetKind()
		if objKind == daemonSetKind && obj.GetName() == routerDaemonSetName {
			router := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &router)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(router.GetName()).To(Equal(routerDaemonSetName))

			// Verify the multus annotation is not present when not specified
			annotations := router.Spec.Template.Annotations
			if annotations != nil {
				g.Expect(annotations["k8s.v1.cni.cncf.io/networks"]).To(BeEmpty())
			}
			routerFound = true
		}
	}
	g.Expect(routerFound).To(BeTrue())
}

/*
func validateObject(testcase, name string, obj *unstructured.Unstructured) error {
	goldenFile := filepath.Join("testdata", testcase+"-"+name+".golden")
	j, err := json.MarshalIndent(obj, "", "    ")
	if err != nil {
		return err
	}
	if *update {
		if err := os.WriteFile(goldenFile, j, 0644); err != nil {
			return err
		}
	}

	expected, err := os.ReadFile(goldenFile)
	if err != nil {
		return err
	}

	if !cmp.Equal(expected, j) {
		return fmt.Errorf("unexpected manifest (-want +got):\n%s", cmp.Diff(string(expected), string(j)))
	}
	return nil
}*/

func TestParseChartWithMasterTolerations(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	tests := []struct {
		name              string
		runOnMaster       bool
		expectTolerations bool
	}{
		{
			name:              "runOnMaster enabled",
			runOnMaster:       true,
			expectTolerations: true,
		},
		{
			name:              "runOnMaster disabled",
			runOnMaster:       false,
			expectTolerations: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			openperouter := &operatorapi.OpenPERouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openperouter",
					Namespace: openperouterTestNamespace,
				},
				Spec: operatorapi.OpenPERouterSpec{
					LogLevel:    "info",
					RunOnMaster: tt.runOnMaster,
				},
			}

			objs, err := chart.Objects(defaultEnvConfig, openperouter)
			g.Expect(err).ToNot(HaveOccurred())

			for _, obj := range objs {
				objKind := obj.GetKind()
				if objKind == daemonSetKind && (obj.GetName() == controllerDaemonSetName || obj.GetName() == routerDaemonSetName) {
					ds := appsv1.DaemonSet{}
					err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &ds)
					g.Expect(err).ToNot(HaveOccurred())

					hasMasterToleration := false
					for _, tol := range ds.Spec.Template.Spec.Tolerations {
						if (tol.Key == "node-role.kubernetes.io/master" || tol.Key == "node-role.kubernetes.io/control-plane") &&
							tol.Effect == v1.TaintEffectNoSchedule {
							hasMasterToleration = true
							break
						}
					}
					g.Expect(hasMasterToleration).To(Equal(tt.expectTolerations),
						fmt.Sprintf("DaemonSet %s should have master toleration=%v", obj.GetName(), tt.expectTolerations))
				}

				if objKind == deploymentKind && obj.GetName() == nodemarkerDeploymentName {
					deployment := appsv1.Deployment{}
					err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &deployment)
					g.Expect(err).ToNot(HaveOccurred())

					hasMasterToleration := false
					for _, tol := range deployment.Spec.Template.Spec.Tolerations {
						if (tol.Key == "node-role.kubernetes.io/master" || tol.Key == "node-role.kubernetes.io/control-plane") &&
							tol.Effect == v1.TaintEffectNoSchedule {
							hasMasterToleration = true
							break
						}
					}
					g.Expect(hasMasterToleration).To(Equal(tt.expectTolerations),
						fmt.Sprintf("Deployment %s should have master toleration=%v", obj.GetName(), tt.expectTolerations))
				}
			}
		})
	}
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
