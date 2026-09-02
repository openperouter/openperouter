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
	"slices"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	"github.com/openperouter/openperouter/operator/internal/envconfig"
	helmchart "helm.sh/helm/v4/pkg/chart"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
		Repo: "quay.io/openperouter/router",
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
	chartAccessor, err := helmchart.NewAccessor(chart.chart)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(chartAccessor.Name()).To(Equal(openperouterChartName))
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
			LogLevel: new(operatorapi.LogLevelInfo),
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

func TestParseChartRouterHasNoMultusAnnotation(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openperouter",
			Namespace: openperouterTestNamespace,
		},
		Spec: operatorapi.OpenPERouterSpec{
			LogLevel: new(operatorapi.LogLevelInfo),
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

			// The removed Multus underlay integration must not annotate the router pod.
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

func TestParseChartWithGroutEnabled(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openperouter",
			Namespace: openperouterTestNamespace,
		},
		Spec: operatorapi.OpenPERouterSpec{
			LogLevel: new(operatorapi.LogLevelInfo),
			Datapath: new("grout"),
		},
	}

	envConfig := defaultEnvConfig
	envConfig.GroutImage = &envconfig.ImageInfo{
		Repo: "quay.io/openperouter/router",
		Tag:  "test-grout",
	}

	objs, err := chart.Objects(envConfig, openperouter)
	g.Expect(err).ToNot(HaveOccurred())

	var routerFound, controllerFound bool
	for _, obj := range objs {
		objKind := obj.GetKind()
		if objKind == daemonSetKind && obj.GetName() == routerDaemonSetName {
			router := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &router)
			g.Expect(err).ToNot(HaveOccurred())

			containerNames := make([]string, 0, len(router.Spec.Template.Spec.Containers))
			for _, c := range router.Spec.Template.Spec.Containers {
				containerNames = append(containerNames, c.Name)
			}
			g.Expect(containerNames).To(ContainElement("grout"))

			for _, c := range router.Spec.Template.Spec.Containers {
				if c.Name == "grout" {
					g.Expect(c.Image).To(Equal("quay.io/openperouter/router:test-grout"))
					env := map[string]string{}
					for _, e := range c.Env {
						env[e.Name] = e.Value
					}
					g.Expect(env["GROUT_SOCK_PATH"]).To(Equal("/var/run/grout/grout.sock"))
					g.Expect(env["GROUT_MEMPOOL_CHUNK_SIZE"]).To(Equal("2047"))
					g.Expect(env["GROUT_PORT_QUEUE_SIZE"]).To(Equal("128"))
				}
			}
			routerFound = true
		}
		if objKind == daemonSetKind && obj.GetName() == controllerDaemonSetName {
			controller := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &controller)
			g.Expect(err).ToNot(HaveOccurred())

			for _, c := range controller.Spec.Template.Spec.Containers {
				if c.Name == "controller" {
					g.Expect(c.Args).To(ContainElement("--datapath=grout"))
					g.Expect(c.Args).To(ContainElement("--grout-socket=/var/run/grout/grout.sock"))
				}
			}
			controllerFound = true
		}
	}
	g.Expect(routerFound).To(BeTrue())
	g.Expect(controllerFound).To(BeTrue())
}

func TestParseChartWithGroutDisabled(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openperouter",
			Namespace: openperouterTestNamespace,
		},
		Spec: operatorapi.OpenPERouterSpec{
			LogLevel: new(operatorapi.LogLevelInfo),
		},
	}

	objs, err := chart.Objects(defaultEnvConfig, openperouter)
	g.Expect(err).ToNot(HaveOccurred())

	var routerFound, controllerFound bool
	for _, obj := range objs {
		objKind := obj.GetKind()
		if objKind == daemonSetKind && obj.GetName() == routerDaemonSetName {
			router := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &router)
			g.Expect(err).ToNot(HaveOccurred())

			for _, c := range router.Spec.Template.Spec.Containers {
				g.Expect(c.Name).ToNot(Equal("grout"))
			}
			routerFound = true
		}
		if objKind == daemonSetKind && obj.GetName() == controllerDaemonSetName {
			controller := appsv1.DaemonSet{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &controller)
			g.Expect(err).ToNot(HaveOccurred())

			for _, c := range controller.Spec.Template.Spec.Containers {
				if c.Name == "controller" {
					g.Expect(c.Args).ToNot(ContainElement("--datapath=grout"))
				}
			}
			controllerFound = true
		}
	}
	g.Expect(routerFound).To(BeTrue())
	g.Expect(controllerFound).To(BeTrue())
}

func TestParseChartWithSchedulingPrimitives(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	testTolerations := []v1.Toleration{{
		Key:      "dedicated",
		Operator: v1.TolerationOpEqual,
		Value:    "openperouter",
		Effect:   v1.TaintEffectNoSchedule,
	}}
	testNodeSelector := map[string]string{"kubernetes.io/os": "linux"}
	terms := []v1.NodeSelectorTerm{{MatchExpressions: []v1.NodeSelectorRequirement{{
		Key: "kubernetes.io/os", Operator: v1.NodeSelectorOpIn, Values: []string{"linux"},
	}}}}
	testAffinity := &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: terms,
		}},
	}

	tests := []struct {
		name               string
		spec               operatorapi.OpenPERouterSpec
		expectTolerations  []v1.Toleration
		expectNodeSelector map[string]string
		expectAffinity     *v1.Affinity
	}{
		{
			name: "expect control-plane toleration (default)",
			spec: operatorapi.OpenPERouterSpec{
				LogLevel: new(operatorapi.LogLevelInfo),
			},
			expectTolerations: []v1.Toleration{
				{
					Key:               "node-role.kubernetes.io/master",
					Operator:          "Exists",
					Value:             "",
					Effect:            "NoSchedule",
					TolerationSeconds: nil,
				},
				{
					Key:               "node-role.kubernetes.io/control-plane",
					Operator:          "Exists",
					Value:             "",
					Effect:            "NoSchedule",
					TolerationSeconds: nil,
				},
			},
		},
		{
			name: "explicit tolerations/nodeSelector/affinity override chart defaults",
			spec: operatorapi.OpenPERouterSpec{
				LogLevel:     new(operatorapi.LogLevelInfo),
				Tolerations:  testTolerations,
				NodeSelector: testNodeSelector,
				Affinity:     testAffinity,
			},
			expectTolerations:  testTolerations,
			expectNodeSelector: testNodeSelector,
			expectAffinity:     testAffinity,
		},
		{
			name: "empty tolerations list opts out of default control-plane tolerations",
			spec: operatorapi.OpenPERouterSpec{
				LogLevel:    new(operatorapi.LogLevelInfo),
				Tolerations: []v1.Toleration{},
			},
			expectTolerations: []v1.Toleration{},
		},
	}
	daemonSets := []string{controllerDaemonSetName, routerDaemonSetName, "hostbridge"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openperouter := &operatorapi.OpenPERouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openperouter",
					Namespace: openperouterTestNamespace,
				},
				Spec: tt.spec,
			}
			objs, err := chart.Objects(defaultEnvConfig, openperouter)
			g.Expect(err).ToNot(HaveOccurred())

			var podSpecs []v1.PodSpec
			for _, obj := range objs {
				objKind := obj.GetKind()
				if objKind == daemonSetKind && slices.Index(daemonSets, obj.GetName()) != -1 {
					ds := appsv1.DaemonSet{}
					err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &ds)
					g.Expect(err).ToNot(HaveOccurred())
					podSpecs = append(podSpecs, ds.Spec.Template.Spec)
				}
				if objKind == deploymentKind && obj.GetName() == nodemarkerDeploymentName {
					deployment := appsv1.Deployment{}
					err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &deployment)
					g.Expect(err).ToNot(HaveOccurred())
					podSpecs = append(podSpecs, deployment.Spec.Template.Spec)
				}
			}
			for _, podSpec := range podSpecs {
				if tt.expectTolerations != nil {
					g.Expect(podSpec.Tolerations).To(ConsistOf(tt.expectTolerations))
				}
				if tt.expectNodeSelector != nil {
					g.Expect(podSpec.NodeSelector).To(Equal(tt.expectNodeSelector))
				}
				if tt.expectAffinity != nil {
					g.Expect(equality.Semantic.DeepEqual(podSpec.Affinity, tt.expectAffinity)).To(BeTrue())
				}
			}
		})
	}
}

func TestParseChartWithBGPListenLimit(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())
	limit := int32(512)
	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openperouter",
			Namespace: openperouterTestNamespace,
		},
		Spec: operatorapi.OpenPERouterSpec{
			BGPListenLimit: &limit,
		},
	}

	objs, err := chart.Objects(defaultEnvConfig, openperouter)
	g.Expect(err).ToNot(HaveOccurred())

	controllerFound := false
	for _, obj := range objs {
		if obj.GetKind() != daemonSetKind || obj.GetName() != controllerDaemonSetName {
			continue
		}
		controller := appsv1.DaemonSet{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &controller)
		g.Expect(err).ToNot(HaveOccurred())
		found := false
		for _, c := range controller.Spec.Template.Spec.Containers {
			for _, arg := range c.Args {
				if arg == fmt.Sprintf("--bgplistenlimit=%d", limit) {
					found = true
				}
			}
		}
		g.Expect(found).To(BeTrue(), "controller daemonset has no --bgplistenlimit arg")
		controllerFound = true
	}
	g.Expect(controllerFound).To(BeTrue())
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
