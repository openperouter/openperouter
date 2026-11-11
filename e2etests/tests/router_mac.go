// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("L3VNI router MAC", Label("router-mac"), PodModeOnly, func() {
	It("uses a different MAC per node and shares it across that node's L3VNI bridges", func() {
		cs := k8sclient.New()
		Expect(Updater.CleanAll()).To(Succeed())
		DeferCleanup(func() { Expect(Updater.CleanAll()).To(Succeed()) })
		DeferCleanup(func() { dumpIfFails(cs) })

		vnis := []v1alpha1.L3VNI{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
				Spec:       v1alpha1.L3VNISpec{VRF: "red", VNI: 100},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: openperouter.Namespace},
				Spec:       v1alpha1.L3VNISpec{VRF: "blue", VNI: 200},
			},
		}
		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay(infra.Default)}, L3VNIs: vnis,
		})).To(Succeed())

		Eventually(func(g Gomega) {
			nodes, err := k8s.GetNodes(cs)
			g.Expect(err).To(Succeed())
			g.Expect(len(nodes)).To(BeNumerically(">=", 2), "at least two nodes are required to verify distinct MACs")
			routers, err := openperouter.Get(cs, false)
			g.Expect(err).To(Succeed())
			owners := make(map[string]string, len(nodes))
			for _, node := range nodes {
				origin, err := routers.ExecutorForNode(node.Name)
				g.Expect(err).To(Succeed())
				var nodeMAC string
				for _, vni := range vnis {
					mac, err := l3VNIBridgeMAC(origin, vni.Spec.VNI)
					g.Expect(err).To(Succeed())
					g.Expect(mac).NotTo(BeEmpty(), "%s/VNI %d", node.Name, vni.Spec.VNI)
					if nodeMAC == "" {
						nodeMAC = mac
					}
					g.Expect(mac).To(Equal(nodeMAC), "L3VNI bridges on %s must share one MAC", node.Name)
				}
				owner, exists := owners[nodeMAC]
				g.Expect(exists).To(BeFalse(), "nodes %s and %s share router MAC %s", owner, node.Name, nodeMAC)
				owners[nodeMAC] = node.Name
			}
		}, 3*time.Minute, time.Second).Should(Succeed())
	})
})

func l3VNIBridgeMAC(exec executor.Executor, vni int32) (string, error) {
	output, err := exec.Exec("ip", "-j", "link", "show", "dev", fmt.Sprintf("br-pe-%d", vni))
	if err != nil {
		return "", err
	}
	var links []struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(output), &links); err != nil {
		return "", err
	}
	if len(links) != 1 {
		return "", fmt.Errorf("expected one bridge for VNI %d, got %d", vni, len(links))
	}
	return links[0].Address, nil
}
