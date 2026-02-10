// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"fmt"
	"strings"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	// Static configuration directory on the host (from systemdmode/generate_systemd.sh)
	// The systemd setup mounts /var/lib/openperouter -> /etc/openperouter in the container
	hostConfigDir = "/var/lib/openperouter/configs"
	// Mount path inside the helper pod
	podConfigMount = "/configs"
)

// This test validates hybrid mode where configuration comes from both
// static files and Kubernetes API, testing the complete lifecycle:
// 1. Start with API-only configuration (underlay + blue VNI)
// 2. Add static file configuration (red VNI)
// 3. Verify both work together
// 4. Remove static file
// 5. Verify only API configuration remains
var _ = Describe("Hybrid mode: static files and API configuration", Label("systemdmode"), Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers
	var configPods []*corev1.Pod
	var frrk8sPods []*corev1.Pod
	var frrK8sConfigBlue []frrk8sv1beta1.FRRConfiguration
	var frrK8sConfigRed []frrk8sv1beta1.FRRConfiguration

	// Route prefixes from leaves
	leafAVRFBluePrefixes := []string{"192.168.21.0/24", "2001:db8:21::/64"}
	leafBVRFBluePrefixes := []string{"192.169.21.0/24", "2001:db8:169:21::/64"}
	leafAVRFRedPrefixes := []string{"192.168.20.0/24", "2001:db8:20::/64"}
	leafBVRFRedPrefixes := []string{"192.169.20.0/24", "2001:db8:169:20::/64"}
	emptyPrefixes := []string{}

	// Blue VNI from API
	vniBlueFromAPI := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "blue",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "blue",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64515,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.11.0/24",
					IPv6: "2001:db8:2::/64",
				},
			},
			VNI: 200,
		},
	}

	// Red VNI from static file
	vniRedFromFile := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "red",
			Namespace: openperouter.Namespace,
		},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red",
			HostSession: &v1alpha1.HostSession{
				ASN:     64514,
				HostASN: 64515,
				LocalCIDR: v1alpha1.LocalCIDRConfig{
					IPv4: "192.169.10.0/24",
					IPv6: "2001:db8:1::/64",
				},
			},
			VNI: 100,
		},
	}

	staticRedVNIYAML := `l3vnis:
  - vrf: red
    hostSession:
      asn: 64514
      hostASN: 64515
      localCIDR:
        ipv4: "192.169.10.0/24"
        ipv6: "2001:db8:1::/64"
    vni: 100
`

	BeforeAll(func() {
		var err error

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())

		cs = k8sclient.New()
		routers, err = openperouter.Get(cs, HostMode)
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(GinkgoWriter)

		frrk8sPods, err = frrk8s.Pods(cs)
		Expect(err).NotTo(HaveOccurred())
		Expect(frrk8sPods).NotTo(BeEmpty(), "Need FRR-K8s pods for BGP route validation")

		frrK8sConfigBlue, err = frrk8s.ConfigFromHostSession(*vniBlueFromAPI.Spec.HostSession, vniBlueFromAPI.Name)
		Expect(err).NotTo(HaveOccurred())

		frrK8sConfigRed, err = frrk8s.ConfigFromHostSession(*vniRedFromFile.Spec.HostSession, vniRedFromFile.Name)
		Expect(err).NotTo(HaveOccurred())

		By("Creating config helper DaemonSet and waiting for pods to be ready")
		configPods, err = createConfigHelperDaemonSet(cs)
		Expect(err).NotTo(HaveOccurred())

		By("Cleaning any existing static configuration files on all nodes")
		for _, pod := range configPods {
			_, err = execInConfigPod(pod, fmt.Sprintf("rm -f %s/openpe_*.yaml", podConfigMount))
			Expect(err).NotTo(HaveOccurred())
		}

		// Create Underlay + Blue VNI via API
		By("Creating underlay and blue VNI via API")
		err = Updater.Update(config.Resources{
			Underlays:         []v1alpha1.Underlay{infra.Underlay},
			L3VNIs:            []v1alpha1.L3VNI{vniBlueFromAPI},
			FRRConfigurations: frrK8sConfigBlue,
		})
		Expect(err).NotTo(HaveOccurred())

		// Wait for blue VNI FRR sessions to be established
		validateFRRK8sSessionForHostSession(vniBlueFromAPI.Name, *vniBlueFromAPI.Spec.HostSession, Established, frrk8sPods...)
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

		Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
		Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())

		err = Updater.CleanAll()
		Expect(err).NotTo(HaveOccurred())
	})

	It("works with API-only config, adds static file config, then removes it", func() {
		ShouldExist := true
		redVNIPath := fmt.Sprintf("%s/openpe_vni_red.yaml", podConfigMount)

		By("advertising routes from the leaves for VRF Blue - VNI 200")
		Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, leafAVRFBluePrefixes)).To(Succeed())
		Expect(infra.LeafBConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, leafBVRFBluePrefixes)).To(Succeed())

		By("checking blue VNI routes are propagated via BGP")
		for _, frrk8sPod := range frrk8sPods {
			checkBGPPrefixesForHostSession(frrk8sPod, *vniBlueFromAPI.Spec.HostSession, leafAVRFBluePrefixes, ShouldExist)
			checkBGPPrefixesForHostSession(frrk8sPod, *vniBlueFromAPI.Spec.HostSession, leafBVRFBluePrefixes, ShouldExist)
		}

		By("creating static red VNI file on all nodes")
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("cat > %s << 'EOF'\n%s\nEOF", redVNIPath, staticRedVNIYAML))
			Expect(err).NotTo(HaveOccurred())
		}

		By("verifying static files were created on all nodes")
		for _, pod := range configPods {
			Eventually(func() error {
				output, err := execInConfigPod(pod, fmt.Sprintf("cat %s", redVNIPath))
				if err != nil {
					return err
				}
				if !strings.Contains(output, "red") {
					return fmt.Errorf("file does not contain expected content")
				}
				return nil
			}, "10s", "1s").Should(Succeed())
		}

		By("applying FRR configuration for red VNI")
		err := Updater.Update(config.Resources{
			Underlays:         []v1alpha1.Underlay{infra.Underlay},
			L3VNIs:            []v1alpha1.L3VNI{vniBlueFromAPI},
			FRRConfigurations: append(frrK8sConfigBlue, frrK8sConfigRed...),
		})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for red VNI FRR sessions to be established")
		validateFRRK8sSessionForHostSession(vniRedFromFile.Name, *vniRedFromFile.Spec.HostSession, Established, frrk8sPods...)

		By("advertising routes from the leaves for both VRFs")
		Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, leafAVRFRedPrefixes, leafAVRFBluePrefixes)).To(Succeed())
		Expect(infra.LeafBConfig.ChangePrefixes(emptyPrefixes, leafBVRFRedPrefixes, leafBVRFBluePrefixes)).To(Succeed())

		By("checking both blue and red VNI routes are propagated via BGP")
		for _, frrk8sPod := range frrk8sPods {
			// Blue VNI routes
			checkBGPPrefixesForHostSession(frrk8sPod, *vniBlueFromAPI.Spec.HostSession, leafAVRFBluePrefixes, ShouldExist)
			checkBGPPrefixesForHostSession(frrk8sPod, *vniBlueFromAPI.Spec.HostSession, leafBVRFBluePrefixes, ShouldExist)
			// Red VNI routes
			checkBGPPrefixesForHostSession(frrk8sPod, *vniRedFromFile.Spec.HostSession, leafAVRFRedPrefixes, ShouldExist)
			checkBGPPrefixesForHostSession(frrk8sPod, *vniRedFromFile.Spec.HostSession, leafBVRFRedPrefixes, ShouldExist)
		}

		By("deleting static red VNI file from all nodes")
		for _, pod := range configPods {
			_, err := execInConfigPod(pod, fmt.Sprintf("rm -f %s", redVNIPath))
			Expect(err).NotTo(HaveOccurred())
		}

		By("checking red VNI routes are NO LONGER propagated via BGP")
		for _, frrk8sPod := range frrk8sPods {
			checkBGPPrefixesForHostSession(frrk8sPod, *vniRedFromFile.Spec.HostSession, leafAVRFRedPrefixes, !ShouldExist)
			checkBGPPrefixesForHostSession(frrk8sPod, *vniRedFromFile.Spec.HostSession, leafBVRFRedPrefixes, !ShouldExist)
		}

		By("checking blue VNI routes still exist")
		for _, frrk8sPod := range frrk8sPods {
			checkBGPPrefixesForHostSession(frrk8sPod, *vniBlueFromAPI.Spec.HostSession, leafAVRFBluePrefixes, ShouldExist)
			checkBGPPrefixesForHostSession(frrk8sPod, *vniBlueFromAPI.Spec.HostSession, leafBVRFBluePrefixes, ShouldExist)
		}
	})
})

// createConfigHelperDaemonSet creates a DaemonSet that can manipulate static configuration files
// by mounting the host's configuration directory on every node. It waits for the pods to be ready
// and returns them.
func createConfigHelperDaemonSet(cs clientset.Interface) ([]*corev1.Pod, error) {
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "config-helper",
			Namespace: openperouter.Namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "config-helper",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "config-helper",
					},
				},
				Spec: corev1.PodSpec{
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
						{
							Key:      "node-role.kubernetes.io/master",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "helper",
							Image:   "busybox:1.36",
							Command: []string{"sleep", "infinity"},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-dir",
									MountPath: podConfigMount,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: hostConfigDir,
									Type: func() *corev1.HostPathType {
										t := corev1.HostPathDirectoryOrCreate
										return &t
									}(),
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := cs.AppsV1().DaemonSets(openperouter.Namespace).Create(
		context.Background(), daemonSet, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	// Wait for pods to be ready
	var readyPods []*corev1.Pod
	Eventually(func() error {
		pods, err := getConfigHelperPods(cs)
		if err != nil {
			return err
		}
		if len(pods) == 0 {
			return fmt.Errorf("no config helper pods found")
		}
		readyPods = pods
		return nil
	}, "2m", "5s").Should(Succeed())

	return readyPods, nil
}

// getConfigHelperPods returns all pods created by the config-helper DaemonSet that are ready.
func getConfigHelperPods(cs clientset.Interface) ([]*corev1.Pod, error) {
	podList, err := cs.CoreV1().Pods(openperouter.Namespace).List(
		context.Background(), metav1.ListOptions{
			LabelSelector: "app=config-helper",
		})
	if err != nil {
		return nil, err
	}

	var readyPods []*corev1.Pod
	for i := range podList.Items {
		pod := &podList.Items[i]
		if k8s.PodIsReady(pod) {
			readyPods = append(readyPods, pod)
		}
	}

	return readyPods, nil
}

// execInConfigPod executes a shell command in the config helper pod
func execInConfigPod(pod *corev1.Pod, command string) (string, error) {
	podExec := executor.ForPod(openperouter.Namespace, pod.Name, "helper")
	return podExec.Exec("sh", "-c", command)
}
