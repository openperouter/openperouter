// SPDX-License-Identifier:Apache-2.0

package systemd_static

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// These tests assume that the cluster was started with the static files available under
// the local testdata dir.
var _ = Describe("Static configuration", Ordered, func() {
	var cs clientset.Interface
	var routers openperouter.Routers

	// Used as a reference, same that we have under testdata
	vniRed := v1alpha1.L3VNI{
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
					IPv4: "192.170.10.0/24",
					IPv6: "2001:db9:1::/64",
				},
			},
			VNI: 100,
		},
	}

	BeforeAll(func() {
		cs = k8sclient.New()
		var err error
		routers, err = openperouter.Get(cs, true) // hostmode = true
		Expect(err).NotTo(HaveOccurred())

		routers.Dump(GinkgoWriter)
	})

	Context("with vnis", func() {
		AfterEach(func() {
			Expect(infra.LeafAConfig.RemovePrefixes()).To(Succeed())
			Expect(infra.LeafBConfig.RemovePrefixes()).To(Succeed())
		})

		It("receives type 5 routes from the fabric", func() {
			emptyPrefixes := []string{}
			leafAVRFRedPrefixes := []string{"192.168.20.0/24", "2001:db8:20::/64"}

			By("announcing type 5 routes on VNI 100 from leafA")
			Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, leafAVRFRedPrefixes, emptyPrefixes)).To(Succeed())
			checkRouteFromLeaf(infra.LeafAConfig, routers, vniRed, mustContain, leafAVRFRedPrefixes)

			By("removing a route from leafA on vni 100")
			Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
		})

		It("dynamically adds and removes a VNI via file watching", func() {
			// Define blue VNI
			vniBlue := v1alpha1.L3VNI{
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

			staticBlueVNIYAML := `l3vnis:
  - vrf: blue
    hostSession:
      asn: 64514
      hostASN: 64515
      localCIDR:
        ipv4: "192.169.11.0/24"
        ipv6: "2001:db8:2::/64"
    vni: 200
`

			emptyPrefixes := []string{}
			leafAVRFBluePrefixes := []string{"192.168.21.0/24", "2001:db8:21::/64"}
			// Host path on the node where systemd mounts it to the controller
			hostConfigPath := "/var/lib/openperouter/configs/openpe_blue.yaml"

			// Create temporary file on test runner
			tempDir, err := os.MkdirTemp("", "openperouter-test-")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tempDir)

			tempFile := filepath.Join(tempDir, "openpe_blue.yaml")
			err = os.WriteFile(tempFile, []byte(staticBlueVNIYAML), 0644)
			Expect(err).NotTo(HaveOccurred())

			// Get all nodes
			nodes, err := k8s.GetNodes(cs)
			Expect(err).NotTo(HaveOccurred())

			By("copying blue VNI file to host filesystem on all nodes using docker cp")
			for _, node := range nodes {
				// Copy to the host filesystem on the node container
				// The systemd setup mounts /var/lib/openperouter -> /etc/openperouter in controller
				_, err := executor.Host.Exec(executor.ContainerRuntime, "cp", tempFile,
					fmt.Sprintf("%s:%s", node.Name, hostConfigPath))
				Expect(err).NotTo(HaveOccurred())
			}

			By("announcing type 5 routes on VNI 200 from leafA")
			Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, leafAVRFBluePrefixes)).To(Succeed())

			By("validating blue VNI routes are received")
			checkRouteFromLeaf(infra.LeafAConfig, routers, vniBlue, mustContain, leafAVRFBluePrefixes)

			By("removing blue VNI file from host filesystem on all nodes")
			for _, node := range nodes {
				// Remove from the host filesystem on the node container
				_, err := executor.Host.Exec(executor.ContainerRuntime, "exec", node.Name,
					"rm", "-f", hostConfigPath)
				Expect(err).NotTo(HaveOccurred())
			}

			By("cleaning up routes from leafA")
			Expect(infra.LeafAConfig.ChangePrefixes(emptyPrefixes, emptyPrefixes, emptyPrefixes)).To(Succeed())
		})
	})
})
