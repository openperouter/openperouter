// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	staticSourceLabelKey   = "openperouter.github.io/source"
	staticSourceLabelValue = "static"
)

// staticUnderlayYAML is a minimal underlay for static mirroring tests.
var staticUnderlayYAML = fmt.Sprintf(`underlays:
  - asn: %d
    interfaces:
      - type: NetworkDevice
        networkDevice:
          interfaceName: toswitch
    neighbors:
      - asn: 64512
        address: "192.168.11.2"
    tunnelEndpoint:
      cidrs:
        - "100.65.0.0/24"
`, infra.Underlay.Spec.ASN)

var staticL3VNIYAML = `l3vnis:
  - vrf: mirror-red
    vni: 8100
    hostsession:
      asn: 64514
      hostasn: 64515
      localcidr:
        ipv4: "192.169.80.0/24"
`

var staticL3VNIUpdatedYAML = `l3vnis:
  - vrf: mirror-red
    vni: 8200
    hostsession:
      asn: 64514
      hostasn: 64515
      localcidr:
        ipv4: "192.169.80.0/24"
`

var _ = Describe("Mirror static config to Kubernetes", Label("systemdmode"), Ordered, func() {
	var cs clientset.Interface
	var configPods []*corev1.Pod
	var nodes []corev1.Node
	var cl client.Reader

	validateL3VNIs := func(expectedVNI int32) func() error {
		return func() error {
			l3vniList := &v1alpha1.L3VNIList{}
			err := cl.List(context.Background(), l3vniList,
				client.InNamespace(openperouter.Namespace),
				client.MatchingLabels{staticSourceLabelKey: staticSourceLabelValue})
			if err != nil {
				return fmt.Errorf("failed to list l3vnis: %w", err)
			}
			if len(l3vniList.Items) < len(nodes) {
				return fmt.Errorf("expected at least %d mirrored l3vnis, got %d",
					len(nodes), len(l3vniList.Items))
			}
			for _, v := range l3vniList.Items {
				if v.Spec.NodeSelector == nil {
					return fmt.Errorf("mirrored L3VNI %s has nil node selector", v.Name)
				}
				if v.Spec.VNI != expectedVNI {
					return fmt.Errorf("mirrored L3VNI %s has VNI %d, expected %d", v.Name, v.Spec.VNI, expectedVNI)
				}
			}
			return nil
		}
	}

	validateL2VNIs := func(expectedVNI int32) func() error {
		return func() error {
			l2vniList := &v1alpha1.L2VNIList{}
			err := cl.List(context.Background(), l2vniList,
				client.InNamespace(openperouter.Namespace),
				client.MatchingLabels{staticSourceLabelKey: staticSourceLabelValue})
			if err != nil {
				return fmt.Errorf("failed to list l2vnis: %w", err)
			}
			if len(l2vniList.Items) == 0 {
				return fmt.Errorf("expected mirrored L2VNIs, got 0")
			}
			for _, v := range l2vniList.Items {
				if v.Spec.VNI == expectedVNI {
					return nil
				}
			}
			return fmt.Errorf("mirrored L2VNI with VNI=%d not found", expectedVNI)
		}
	}

	BeforeAll(func() {
		var err error

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()

		nodes, err = k8s.GetNodes(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(nodes).NotTo(BeEmpty(), "need at least one node")

		cl = Updater.Client()

		By("Creating config helper DaemonSet and waiting for pods to be ready")
		configPods, err = createConfigHelperDaemonSet(cs)
		Expect(err).NotTo(HaveOccurred())

		By("Cleaning any existing static configuration files on all nodes")
		for _, pod := range configPods {
			_, err = execInConfigPod(pod, fmt.Sprintf("rm -f %s/openpe_*.yaml", podConfigMount))
			Expect(err).NotTo(HaveOccurred())
		}
	})

	AfterAll(func() {
		By("Removing static configuration files on all nodes")
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("rm -f %s/openpe_*.yaml", podConfigMount))
			Expect(err).NotTo(HaveOccurred())
		}

		By("Deleting config helper DaemonSet")
		err := cs.AppsV1().DaemonSets(openperouter.Namespace).Delete(
			context.Background(), "config-helper", metav1.DeleteOptions{})
		if err != nil {
			GinkgoWriter.Printf("Warning: failed to delete DaemonSet: %v\n", err)
		}

		By("Waiting for config helper pods to be removed")
		Eventually(func() error {
			pods, err := cs.CoreV1().Pods(openperouter.Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=config-helper",
			})
			if err != nil {
				return err
			}
			if len(pods.Items) > 0 {
				return fmt.Errorf("still waiting for %d config helper pods to be removed", len(pods.Items))
			}
			return nil
		}, 60*time.Second, 1*time.Second).Should(Succeed())

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	writeBaselineStaticFiles := func() {
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("cat > %s/openpe_underlay.yaml << 'EOF'\n%s\nEOF",
				podConfigMount, staticUnderlayYAML))
			Expect(err).NotTo(HaveOccurred())

			_, err = execInConfigPod(pod, fmt.Sprintf("cat > %s/openpe_l3vni.yaml << 'EOF'\n%s\nEOF",
				podConfigMount, staticL3VNIYAML))
			Expect(err).NotTo(HaveOccurred())
		}
	}

	validateUnderlays := func() error {
		underlayList := &v1alpha1.UnderlayList{}
		err := cl.List(context.Background(), underlayList,
			client.InNamespace(openperouter.Namespace),
			client.MatchingLabels{staticSourceLabelKey: staticSourceLabelValue})
		if err != nil {
			return fmt.Errorf("failed to list underlays: %w", err)
		}
		if len(underlayList.Items) != len(nodes) {
			return fmt.Errorf("expected %d mirrored underlays (one per node), got %d",
				len(nodes), len(underlayList.Items))
		}
		for _, u := range underlayList.Items {
			if u.Spec.NodeSelector == nil {
				return fmt.Errorf("mirrored underlay %s has nil node selector", u.Name)
			}
			if u.Spec.ASN != infra.Underlay.Spec.ASN {
				return fmt.Errorf("mirrored underlay %s has ASN %d, expected %d", u.Name, u.Spec.ASN, infra.Underlay.Spec.ASN)
			}
			if len(u.Spec.Interfaces) != 1 ||
				u.Spec.Interfaces[0].NetworkDevice == nil ||
				u.Spec.Interfaces[0].NetworkDevice.InterfaceName != "toswitch" {
				return fmt.Errorf("mirrored underlay %s has interfaces %v, expected a single NetworkDevice toswitch", u.Name, u.Spec.Interfaces)
			}
			if len(u.Spec.Neighbors) != 1 || u.Spec.Neighbors[0].Address == nil || *u.Spec.Neighbors[0].Address != "192.168.11.2" {
				return fmt.Errorf("mirrored underlay %s has unexpected neighbors", u.Name)
			}
			if u.Spec.TunnelEndpoint == nil || len(u.Spec.TunnelEndpoint.CIDRs) != 1 || u.Spec.TunnelEndpoint.CIDRs[0] != "100.65.0.0/24" {
				return fmt.Errorf("mirrored underlay %s has unexpected tunnel endpoint config", u.Name)
			}
		}
		return nil
	}

	BeforeEach(func() {
		writeBaselineStaticFiles()
	})

	AfterEach(func() {
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("rm -f %s/openpe_*.yaml", podConfigMount))
			Expect(err).NotTo(HaveOccurred())
		}

		err := Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	It("mirrors static underlay and L3VNI to Kubernetes CRDs", func() {
		By("waiting for mirrored underlay to appear with static source label")
		Eventually(validateUnderlays, "60s", "2s").Should(Succeed())

		By("waiting for mirrored L3VNI to appear with static source label")
		Eventually(validateL3VNIs(8100), "60s", "2s").Should(Succeed())
	})

	It("updates mirrored L3VNI when static file changes", func() {
		By("waiting for baseline L3VNI to be mirrored")
		Eventually(validateL3VNIs(8100), "60s", "2s").Should(Succeed())

		By("overwriting static L3VNI file with updated VNI on all nodes")
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("cat > %s/openpe_l3vni.yaml << 'EOF'\n%s\nEOF",
				podConfigMount, staticL3VNIUpdatedYAML))
			Expect(err).NotTo(HaveOccurred())
		}

		By("waiting for mirrored L3VNI to show updated VNI=8200")
		Eventually(validateL3VNIs(8200), "30s", "2s").Should(Succeed())
	})

	It("creates new mirrored resource when a new static file is added", func() {
		By("waiting for baseline resources to be mirrored")
		Eventually(validateL3VNIs(8100), "60s", "2s").Should(Succeed())

		By("writing a new L2VNI static file on all nodes")
		l2vniYAML := `l2vnis:
  - vni: 8300
    vxlanport: 4789
    vrf: sl2vni8300
`
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("cat > %s/openpe_l2vni.yaml << 'EOF'\n%s\nEOF",
				podConfigMount, l2vniYAML))
			Expect(err).NotTo(HaveOccurred())
		}

		By("waiting for new mirrored L2VNI to appear")
		Eventually(validateL2VNIs(8300), "30s", "2s").Should(Succeed())
	})

	It("deletes mirrored resource when static file is removed", func() {
		By("writing a L2VNI static file on all nodes")
		l2vniYAML := `l2vnis:
  - vni: 8300
    vxlanport: 4789
    vrf: sl2vni8300
`
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("cat > %s/openpe_l2vni.yaml << 'EOF'\n%s\nEOF",
				podConfigMount, l2vniYAML))
			Expect(err).NotTo(HaveOccurred())
		}

		By("waiting for mirrored L2VNI to appear")
		Eventually(validateL2VNIs(8300), "60s", "2s").Should(Succeed())

		By("removing L2VNI static file from all nodes")
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("rm -f %s/openpe_l2vni.yaml", podConfigMount))
			Expect(err).NotTo(HaveOccurred())
		}

		By("waiting for mirrored L2VNIs to be deleted")
		Eventually(func() error {
			l2vniList := &v1alpha1.L2VNIList{}
			err := cl.List(context.Background(), l2vniList,
				client.InNamespace(openperouter.Namespace),
				client.MatchingLabels{staticSourceLabelKey: staticSourceLabelValue})
			if err != nil {
				return fmt.Errorf("failed to list l2vnis: %w", err)
			}
			if len(l2vniList.Items) > 0 {
				return fmt.Errorf("expected 0 mirrored L2VNIs after file removal, got %d", len(l2vniList.Items))
			}
			return nil
		}, "30s", "2s").Should(Succeed())

		By("verifying underlay mirrored resources still exist")
		Eventually(validateUnderlays, "30s", "2s").Should(Succeed())
	})

	It("re-creates mirrored resource after manual deletion", func() {
		By("waiting for baseline L3VNI to be mirrored")
		l3vniList := &v1alpha1.L3VNIList{}
		Eventually(func() error {
			err := cl.List(context.Background(), l3vniList,
				client.InNamespace(openperouter.Namespace),
				client.MatchingLabels{staticSourceLabelKey: staticSourceLabelValue})
			if err != nil {
				return err
			}
			if len(l3vniList.Items) == 0 {
				return fmt.Errorf("no mirrored L3VNIs found")
			}
			return nil
		}, "60s", "2s").Should(Succeed())

		targetName := l3vniList.Items[0].Name
		targetNs := l3vniList.Items[0].Namespace

		By(fmt.Sprintf("deleting mirrored L3VNI %s via kubectl", targetName))
		toDelete := l3vniList.Items[0].DeepCopy()
		err := Updater.Client().Delete(context.Background(), toDelete)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for mirrored L3VNI to be re-created")
		Eventually(func() error {
			var v v1alpha1.L3VNI
			err := cl.Get(context.Background(), client.ObjectKey{
				Name:      targetName,
				Namespace: targetNs,
			}, &v)
			if err != nil {
				return fmt.Errorf("mirrored L3VNI %s not yet re-created: %w", targetName, err)
			}
			if v.Labels[staticSourceLabelKey] != staticSourceLabelValue {
				return fmt.Errorf("re-created L3VNI missing static source label")
			}
			return nil
		}, "30s", "2s").Should(Succeed())
	})

	It("webhook rejects a new K8s-managed L3VNI with same VNI as mirrored one", func() {
		By("ensuring mirrored L3VNIs exist with VNI=8100")
		Eventually(validateL3VNIs(8100), "60s", "2s").Should(Succeed())

		By("attempting to create a K8s-managed L3VNI with VNI=8100 (same as mirrored)")
		conflictingVNI := v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "conflict-vni-test",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: "conflict-vrf",
				VNI: 8100,
				HostSession: &v1alpha1.HostSession{
					ASN:     64514,
					HostASN: new(int64(64518)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: new("192.169.90.0/24"),
					},
				},
			},
		}
		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{conflictingVNI},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("duplicate vni"))
	})

	It("webhook allows a new K8s-managed L3VNI that does not conflict with mirrored", func() {
		By("ensuring mirrored L3VNIs exist")
		Eventually(validateL3VNIs(8100), "60s", "2s").Should(Succeed())

		nonConflicting := v1alpha1.L3VNI{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "noconflict-vni-test",
				Namespace: openperouter.Namespace,
			},
			Spec: v1alpha1.L3VNISpec{
				VRF: "noconflict-vrf",
				VNI: 9999,
				HostSession: &v1alpha1.HostSession{
					ASN:     64514,
					HostASN: new(int64(64519)),
					LocalCIDR: v1alpha1.LocalCIDRConfig{
						IPv4: new("192.169.91.0/24"),
					},
				},
			},
		}
		err := Updater.Update(config.Resources{
			L3VNIs: []v1alpha1.L3VNI{nonConflicting},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("reverts mirrored L3VNI spec after manual edit", func() {
		By("waiting for baseline L3VNI to be mirrored")
		var l3vni v1alpha1.L3VNI
		Eventually(func() error {
			l3vniList := &v1alpha1.L3VNIList{}
			err := cl.List(context.Background(), l3vniList,
				client.InNamespace(openperouter.Namespace),
				client.MatchingLabels{staticSourceLabelKey: staticSourceLabelValue})
			if err != nil {
				return err
			}
			if len(l3vniList.Items) == 0 {
				return fmt.Errorf("no mirrored L3VNIs found")
			}
			l3vni = l3vniList.Items[0]
			return nil
		}, "60s", "2s").Should(Succeed())

		originalVNI := l3vni.Spec.VNI
		Expect(originalVNI).NotTo(BeZero())

		By(fmt.Sprintf("patching mirrored L3VNI %s VNI from %d to 9876", l3vni.Name, originalVNI))
		patch, err := json.Marshal(map[string]any{
			"spec": map[string]any{
				"vni": 9876,
			},
		})
		Expect(err).NotTo(HaveOccurred())
		err = Updater.Client().Patch(context.Background(), &l3vni, client.RawPatch(types.MergePatchType, patch))
		Expect(err).NotTo(HaveOccurred())

		By(fmt.Sprintf("waiting for VNI to revert to %d", originalVNI))
		Eventually(func() int32 {
			var v v1alpha1.L3VNI
			err := cl.Get(context.Background(), client.ObjectKey{
				Name:      l3vni.Name,
				Namespace: l3vni.Namespace,
			}, &v)
			if err != nil {
				return 0
			}
			return v.Spec.VNI
		}, "30s", "2s").Should(Equal(originalVNI))
	})

	It("webhook rejects a new K8s-managed L2VNI with same VNI as mirrored one", func() {
		By("writing a static L2VNI file on all nodes")
		l2vniYAML := `l2vnis:
  - vni: 8400
    vxlanport: 4789
    vrf: sl2vni8400
`
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("cat > %s/openpe_l2vni_webhook.yaml << 'EOF'\n%s\nEOF",
				podConfigMount, l2vniYAML))
			Expect(err).NotTo(HaveOccurred())
		}

		By("waiting for mirrored L2VNI to appear")
		Eventually(validateL2VNIs(8400), "60s", "2s").Should(Succeed())

		By("attempting to create a K8s-managed L2VNI with VNI=8400 (same as mirrored)")
		err := Updater.Update(config.Resources{
			L2VNIs: []v1alpha1.L2VNI{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cnfl2test",
						Namespace: openperouter.Namespace,
					},
					Spec: v1alpha1.L2VNISpec{
						VNI:       8400,
						VXLanPort: new(int32(4789)),
					},
				},
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("duplicate vni"))

		By("cleaning up static L2VNI file")
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("rm -f %s/openpe_l2vni_webhook.yaml", podConfigMount))
			Expect(err).NotTo(HaveOccurred())
		}

		By("waiting for mirrored L2VNI to be cleaned up")
		Eventually(func() error {
			l2vniList := &v1alpha1.L2VNIList{}
			err := cl.List(context.Background(), l2vniList,
				client.InNamespace(openperouter.Namespace),
				client.MatchingLabels{staticSourceLabelKey: staticSourceLabelValue})
			if err != nil {
				return err
			}
			for _, v := range l2vniList.Items {
				if v.Spec.VNI == 8400 {
					return fmt.Errorf("mirrored L2VNI VNI=8400 still exists")
				}
			}
			return nil
		}, "30s", "2s").Should(Succeed())
	})

	It("webhook rejects a new K8s-managed underlay when mirrored one already exists on same node", func() {
		By("waiting for mirrored underlay to exist")
		Eventually(validateUnderlays, "60s", "2s").Should(Succeed())

		By("attempting to create a K8s-managed underlay (conflicts: can't have >1 underlay per node)")
		err := Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "conflict-underlay-test",
						Namespace: openperouter.Namespace,
					},
					Spec: v1alpha1.UnderlaySpec{
						ASN: 64520,
						Neighbors: []v1alpha1.Neighbor{
							{
								ASN:     new(int64(64521)),
								Address: new("192.168.99.2"),
							},
						},
						Interfaces: []v1alpha1.UnderlayInterface{
							{
								Type:          "NetworkDevice",
								NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: "eth0"},
							},
						},
						TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
							CIDRs: []string{"100.66.0.0/24"},
						},
					},
				},
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("can't have more than one underlay per node"))
	})
})
