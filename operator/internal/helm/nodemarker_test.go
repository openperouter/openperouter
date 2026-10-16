// SPDX-License-Identifier:Apache-2.0

package helm

import (
	"testing"

	. "github.com/onsi/gomega"
	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestParseChartNodemarkerHighAvailability(t *testing.T) {
	g := NewGomegaWithT(t)
	chart, err := NewChart(testChartPath, openperouterChartName, openperouterTestNamespace)
	g.Expect(err).ToNot(HaveOccurred())
	openperouter := &operatorapi.OpenPERouter{
		ObjectMeta: metav1.ObjectMeta{Name: "openperouter", Namespace: openperouterTestNamespace},
	}
	objs, err := chart.Objects(defaultEnvConfig, openperouter)
	g.Expect(err).ToNot(HaveOccurred())

	var deployment *appsv1.Deployment
	var pdb *policyv1.PodDisruptionBudget
	for _, obj := range objs {
		switch {
		case obj.GetKind() == deploymentKind && obj.GetName() == nodemarkerDeploymentName:
			deployment = &appsv1.Deployment{}
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), deployment)).To(Succeed())
		case obj.GetKind() == "PodDisruptionBudget":
			pdb = &policyv1.PodDisruptionBudget{}
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), pdb)).To(Succeed())
		}
	}
	g.Expect(deployment).ToNot(BeNil())
	g.Expect(pdb).ToNot(BeNil())

	g.Expect(deployment.Spec.Replicas).To(HaveValue(Equal(int32(2))))
	g.Expect(deployment.Spec.Strategy.RollingUpdate).ToNot(BeNil())
	g.Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.IntValue()).To(Equal(0))

	podLabels := labels.Set(deployment.Spec.Template.Labels)
	g.Expect(pdb.Spec.MinAvailable.IntValue()).To(Equal(1))
	pdbSelector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(pdbSelector.Matches(podLabels)).To(BeTrue(), "the PDB does not select the nodemarker pods")

	podSpec := deployment.Spec.Template.Spec
	g.Expect(podSpec.Affinity).ToNot(BeNil())
	g.Expect(podSpec.Affinity.PodAntiAffinity).ToNot(BeNil())
	terms := podSpec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	g.Expect(terms).To(HaveLen(1))
	g.Expect(terms[0].TopologyKey).To(Equal("kubernetes.io/hostname"))
	termSelector, err := metav1.LabelSelectorAsSelector(terms[0].LabelSelector)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(termSelector.Matches(podLabels)).To(BeTrue(), "the anti-affinity does not select the nodemarker pods")

	g.Expect(podSpec.Containers).To(HaveLen(1))
	container := podSpec.Containers[0]
	g.Expect(container.Args).To(ContainElement("--leader-elect=true"))
	g.Expect(container.Lifecycle).ToNot(BeNil())
	g.Expect(container.Lifecycle.PreStop).ToNot(BeNil())
	g.Expect(container.Lifecycle.PreStop.Exec.Command).To(Equal([]string{"sleep", "5"}))
	g.Expect(container.LivenessProbe).ToNot(BeNil())
	g.Expect(container.LivenessProbe.TimeoutSeconds).To(Equal(int32(5)))
	g.Expect(container.LivenessProbe.FailureThreshold).To(Equal(int32(6)))
}
