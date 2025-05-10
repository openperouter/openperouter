// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

var (
	ValidatorPath string
)

const (
	underlayTestSelector      = "EXTERNAL.*underlay"
	vniConfiguredTestSelector = "EXTERNAL.*vni.*configured"
	vniDeletedTestSelector    = "EXTERNAL.*vni.*deleted"
)

var _ = ginkgo.Describe("Router Host configuration", func() {
	var cs clientset.Interface
	routerPods := []*corev1.Pod{}

	underlay := v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "underlay",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.UnderlaySpec{
			ASN:      64514,
			VTEPCIDR: "100.65.0.0/24",
			Nics:     []string{"toswitch"},
			Neighbors: []v1alpha1.Neighbor{
				{
					ASN:     64514,
					Address: "192.168.11.2",
				},
			},
		},
	}

	vni100 := v1alpha1.VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "first",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.VNISpec{
			ASN:       64514,
			VNI:       100,
			LocalCIDR: "192.169.10.0/24",
			HostASN:   ptr.To(uint32(64515)),
		},
	}
	vni200 := v1alpha1.VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "second",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.VNISpec{
			ASN:       64514,
			VNI:       200,
			LocalCIDR: "192.169.11.0/24",
			HostASN:   ptr.To(uint32(64515)),
		},
	}

	ginkgo.BeforeEach(func() {
		cs = k8sclient.New()
		ginkgo.By("ensuring the validator is in all the pods")
		var err error
		routerPods, err = openperouter.RouterPods(cs)
		Expect(err).NotTo(HaveOccurred())
		for _, pod := range routerPods {
			ensureValidator(cs, pod)
		}

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
	})

	ginkgo.AfterEach(func() {
		dumpIfFails(cs)
		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
		ginkgo.By("waiting for the router pod to rollout after removing the underlay")
		Eventually(func() error {
			return openperouter.DaemonsetRolled(cs, routerPods)
		}, time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

	ginkgo.It("is applied correctly", func() {
		err := Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				underlay,
			},
			VNIs: []v1alpha1.VNI{
				vni100,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		for _, p := range routerPods {
			ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

			vtepIP := vtepIPForPod(cs, underlay.Spec.VTEPCIDR, p)
			validateConfig(vniParams{
				VRF:       vni100.Name,
				VethNSIP:  "192.169.10.0/24",
				VNI:       100,
				VXLanPort: 4789,
				VTEPIP:    vtepIP,
			}, vniConfiguredTestSelector, p)

			ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

			validateConfig(underlayParams{
				UnderlayInterface: "toswitch",
				VtepIP:            vtepIP,
			}, underlayTestSelector, p)
		}
	})

	ginkgo.It("works with two vnis and deletion", func() {
		err := Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				underlay,
			},
			VNIs: []v1alpha1.VNI{
				vni100,
				vni200,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		for _, p := range routerPods {
			ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

			vtepIP := vtepIPForPod(cs, underlay.Spec.VTEPCIDR, p)
			validateConfig(vniParams{
				VRF:       vni100.Name,
				VethNSIP:  vni100.Spec.LocalCIDR,
				VNI:       vni100.Spec.VNI,
				VXLanPort: 4789,
				VTEPIP:    vtepIP,
			}, vniConfiguredTestSelector, p)

			validateConfig(vniParams{
				VRF:       vni200.Name,
				VethNSIP:  vni200.Spec.LocalCIDR,
				VNI:       vni200.Spec.VNI,
				VXLanPort: 4789,
				VTEPIP:    vtepIP,
			}, vniConfiguredTestSelector, p)
		}

		ginkgo.By("delete the first vni")
		err = Updater.Client().Delete(context.Background(), &vni100)
		Expect(err).NotTo(HaveOccurred())

		for _, p := range routerPods {
			ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

			vtepIP := vtepIPForPod(cs, underlay.Spec.VTEPCIDR, p)
			validateConfig(vniParams{
				VRF:       vni200.Name,
				VethNSIP:  vni200.Spec.LocalCIDR,
				VNI:       vni200.Spec.VNI,
				VXLanPort: 4789,
				VTEPIP:    vtepIP,
			}, vniConfiguredTestSelector, p)

			ginkgo.By(fmt.Sprintf("validating VNI is deleted for pod %s", p.Name))
			validateConfig(vniParams{
				VRF:       vni100.Name,
				VethNSIP:  vni100.Spec.LocalCIDR,
				VNI:       vni100.Spec.VNI,
				VXLanPort: 4789,
				VTEPIP:    vtepIP,
			}, vniDeletedTestSelector, p)
		}

		for _, p := range routerPods {
			vtepIP := vtepIPForPod(cs, underlay.Spec.VTEPCIDR, p)

			ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

			validateConfig(underlayParams{
				UnderlayInterface: "toswitch",
				VtepIP:            vtepIP,
			}, underlayTestSelector, p)
		}
	})

	ginkgo.It("works while editing the vni parameters", func() {
		resources := config.Resources{
			Underlays: []v1alpha1.Underlay{
				underlay,
			},
			VNIs: []v1alpha1.VNI{
				*vni100.DeepCopy(),
			},
		}

		err := Updater.Update(resources)
		Expect(err).NotTo(HaveOccurred())

		for _, p := range routerPods {
			ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

			vtepIP := vtepIPForPod(cs, underlay.Spec.VTEPCIDR, p)
			validateConfig(vniParams{
				VRF:       vni100.Name,
				VethNSIP:  vni100.Spec.LocalCIDR,
				VNI:       vni100.Spec.VNI,
				VXLanPort: 4789,
				VTEPIP:    vtepIP,
			}, vniConfiguredTestSelector, p)
		}

		ginkgo.By("editing the first vni")

		resources.VNIs[0].Spec.ASN = 64515
		resources.VNIs[0].Spec.VNI = 300
		resources.VNIs[0].Spec.LocalCIDR = "192.171.10.0/24"
		resources.VNIs[0].Spec.HostASN = ptr.To(uint32(64516))
		err = Updater.Update(resources)
		Expect(err).NotTo(HaveOccurred())

		for _, p := range routerPods {
			ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

			vtepIP := vtepIPForPod(cs, underlay.Spec.VTEPCIDR, p)
			changedVni := resources.VNIs[0]
			validateConfig(vniParams{
				VRF:       changedVni.Name,
				VethNSIP:  changedVni.Spec.LocalCIDR,
				VNI:       changedVni.Spec.VNI,
				VXLanPort: 4789,
				VTEPIP:    vtepIP,
			}, vniConfiguredTestSelector, p)
		}

		for _, p := range routerPods {
			vtepIP := vtepIPForPod(cs, underlay.Spec.VTEPCIDR, p)

			ginkgo.By(fmt.Sprintf("validating Underlay for pod %s", p.Name))

			validateConfig(underlayParams{
				UnderlayInterface: "toswitch",
				VtepIP:            vtepIP,
			}, underlayTestSelector, p)
		}
	})

	ginkgo.It("works while editing the underlay parameters", func() {
		resources := config.Resources{
			Underlays: []v1alpha1.Underlay{
				*underlay.DeepCopy(),
			},
			VNIs: []v1alpha1.VNI{
				vni100,
			},
		}

		err := Updater.Update(resources)
		Expect(err).NotTo(HaveOccurred())

		validate := func(vtepCidr string) {
			for _, p := range routerPods {
				ginkgo.By(fmt.Sprintf("validating VNI for pod %s", p.Name))

				vtepIP := vtepIPForPod(cs, vtepCidr, p)
				validateConfig(vniParams{
					VRF:       vni100.Name,
					VethNSIP:  vni100.Spec.LocalCIDR,
					VNI:       vni100.Spec.VNI,
					VXLanPort: 4789,
					VTEPIP:    vtepIP,
				}, vniConfiguredTestSelector, p)

				ginkgo.By(fmt.Sprintf("validating underlay for pod %s", p.Name))
				validateConfig(underlayParams{
					UnderlayInterface: "toswitch",
					VtepIP:            vtepIP,
				}, underlayTestSelector, p)
			}
		}

		validate(underlay.Spec.VTEPCIDR)

		ginkgo.By("editing the vtep cidr vni")

		newCidr := "100.64.0.0/24"
		resources.Underlays[0].Spec.VTEPCIDR = newCidr
		err = Updater.Update(resources)
		Expect(err).NotTo(HaveOccurred())

		validate(newCidr)

		ginkgo.By("editing the underlay nic (to non existent one)")
		resources.Underlays[0].Spec.Nics[0] = "foo"
		err = Updater.Update(resources)
		Expect(err).NotTo(HaveOccurred())

		ginkgo.By("waiting for the routers to be rolled out again")
		Eventually(func() error {
			return openperouter.DaemonsetRolled(cs, routerPods)
		}, time.Minute, time.Second).ShouldNot(HaveOccurred())
	})

})

type vniParams struct {
	VRF       string `json:"vrf"`
	VTEPIP    string `json:"vtepip"`
	VethNSIP  string `json:"vethnsip"`
	VNI       uint32 `json:"vni"`
	VXLanPort int    `json:"vxlanport"`
}

type underlayParams struct {
	UnderlayInterface string `json:"underlay_interface"`
	VtepIP            string `json:"vtep_ip"`
}

func validateConfig[T any](config T, test string, pod *corev1.Pod) {
	fileToValidate := sendConfigToValidate(pod, config)
	Eventually(func() error {
		exec := executor.ForPod(pod.Namespace, pod.Name, "frr")
		res, err := exec.Exec("/validatehost", "--ginkgo.focus", test, "--paramsfile", fileToValidate)
		if err != nil {
			return fmt.Errorf("failed to validate test %s : %s %w", test, res, err)
		}
		return nil
	}, time.Minute, time.Second).ShouldNot(HaveOccurred())
}

func ensureValidator(cs clientset.Interface, pod *corev1.Pod) {
	if pod.Annotations != nil && pod.Annotations["validator"] == "true" {
		return
	}
	dst := fmt.Sprintf("%s/%s:/", pod.Namespace, pod.Name)
	fullargs := []string{"cp", ValidatorPath, dst}
	_, err := exec.Command(executor.Kubectl, fullargs...).CombinedOutput()
	Expect(err).NotTo(HaveOccurred())

	pod.Annotations["validator"] = "true"
	_, err = cs.CoreV1().Pods(pod.Namespace).Update(context.Background(), pod, metav1.UpdateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func vtepIPForPod(cs clientset.Interface, vtepCIDR string, pod *corev1.Pod) string {
	node, err := k8s.NodeObjectForPod(cs, pod)
	Expect(err).NotTo(HaveOccurred())
	vtepIP, err := openperouter.VtepIPForNode(vtepCIDR, node)
	Expect(err).NotTo(HaveOccurred())
	return vtepIP
}

func sendConfigToValidate[T any](pod *corev1.Pod, toValidate T) string {
	jsonData, err := json.MarshalIndent(toValidate, "", "  ")
	if err != nil {
		panic(err)
	}

	toValidateFile, err := os.CreateTemp(os.TempDir(), "validate-*.json")
	Expect(err).NotTo(HaveOccurred())

	_, err = toValidateFile.Write(jsonData)
	Expect(err).NotTo(HaveOccurred())

	err = k8s.SendFileToPod(toValidateFile.Name(), pod)
	Expect(err).NotTo(HaveOccurred())
	return filepath.Base(toValidateFile.Name())
}
