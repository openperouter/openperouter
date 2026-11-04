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
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/systemd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var _ = Describe("L3VNI router MAC", Label("systemdmode"), Serial, func() {
	It("shares the configured MAC across VNIs and advertises it after drift and bridge recreation", func() {
		if !HostMode || GroutMode {
			Skip("requires the systemd kernel deployment")
		}
		cs := k8sclient.New()
		Expect(Updater.CleanAll()).To(Succeed())
		nodes, err := k8s.GetNodes(cs)
		Expect(err).To(Succeed())
		Expect(nodes).NotTo(BeEmpty())
		macs := make(map[string]string, len(nodes))
		const nodeConfigPath = "/var/lib/openperouter/node-config.yaml"
		for i, node := range nodes {
			nodeExec := executor.ForNode(node.Name)
			original, err := nodeExec.Exec("cat", nodeConfigPath)
			Expect(err).To(Succeed())
			DeferCleanup(func() {
				Expect(writeRouterMACNodeConfig(nodeExec, nodeConfigPath, original)).To(Succeed())
				Expect(systemd.RestartSystemdUnit(nodeExec, "controller.service")).To(Succeed())
			})
			var settings map[string]any
			Expect(yaml.Unmarshal([]byte(original), &settings)).To(Succeed())
			macs[node.Name] = fmt.Sprintf("02:00:00:00:%02x:%02x", (i+1)/256, (i+1)%256)
			settings["routerMAC"] = macs[node.Name]
			updated, err := yaml.Marshal(settings)
			Expect(err).To(Succeed())
			Expect(writeRouterMACNodeConfig(nodeExec, nodeConfigPath, string(updated))).To(Succeed())
			Expect(systemd.RestartSystemdUnit(nodeExec, "controller.service")).To(Succeed())
		}
		DeferCleanup(func() {
			Expect(Updater.CleanAll()).To(Succeed())
			Eventually(func(g Gomega) {
				for _, node := range nodes {
					configured, err := openperouter.UnderlayConfigured(node.Name)
					g.Expect(err).To(Succeed())
					g.Expect(configured).To(BeFalse(), "underlay on %s", node.Name)
				}
			}, 2*time.Minute, time.Second).Should(Succeed())
		})
		// Dump before cleanup removes the configuration under investigation.
		DeferCleanup(func() { dumpIfFails(cs) })

		underlay := infra.Underlay(infra.Default)
		vnis := []v1alpha1.L3VNI{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
				Spec: v1alpha1.L3VNISpec{VRF: "red", VNI: 100, HostSession: &v1alpha1.HostSession{
					ASN: 64514, HostASN: new(int64(64515)), LocalCIDRs: []string{"192.169.10.0/24"},
				}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "blue", Namespace: openperouter.Namespace},
				Spec: v1alpha1.L3VNISpec{VRF: "blue", VNI: 200, HostSession: &v1alpha1.HostSession{
					ASN: 64514, HostASN: new(int64(64515)), LocalCIDRs: []string{"192.169.11.0/24"},
				}},
			},
		}
		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{underlay}, L3VNIs: vnis,
		})).To(Succeed())

		checkMACs := func() {
			Eventually(func(g Gomega) {
				routers, err := openperouter.Get(cs, true)
				g.Expect(err).To(Succeed())
				for _, node := range nodes {
					origin, err := routers.ExecutorForNode(node.Name)
					g.Expect(err).To(Succeed())
					vtep, err := openperouter.GetVtepIPv4ForNode(underlay.Spec.TunnelEndpoint, &node)
					g.Expect(err).To(Succeed())
					for _, vni := range vnis {
						bridge := fmt.Sprintf("br-pe-%d", vni.Spec.VNI)
						output, err := origin.Exec("ip", "-j", "link", "show", "dev", bridge)
						g.Expect(err).To(Succeed())
						var links []struct {
							Address string `json:"address"`
						}
						g.Expect(json.Unmarshal([]byte(output), &links)).To(Succeed())
						g.Expect(links).To(HaveLen(1))
						g.Expect(links[0].Address).To(Equal(macs[node.Name]), "%s/%s", node.Name, bridge)
						prefix, err := openperouter.HostIPFromCIDRForNode(vni.Spec.HostSession.LocalCIDRs[0], &node)
						g.Expect(err).To(Succeed())
						for _, leaf := range []string{infra.LeafA, infra.LeafB} {
							routes, err := frr.EVPNInfo(executor.ForContainer(leaf))
							g.Expect(err).To(Succeed())
							rt := v1alpha1.RouteTarget(fmt.Sprintf("64514:%d", vni.Spec.VNI))
							g.Expect(routes.ContainsType5RouteWithRouterMAC(
								ipfamily.StripCIDRMask(prefix)+"/32", ipfamily.StripCIDRMask(vtep), rt, macs[node.Name],
							)).To(BeTrue(), "%s should receive %s from %s with RMAC %s", leaf, prefix, node.Name, macs[node.Name])
						}
					}
				}
			}, 3*time.Minute, time.Second).Should(Succeed())
		}

		By("checking both L3VNI bridges and their Type-5 advertisements on both leaves")
		checkMACs()
		for _, stage := range []struct {
			name string
			args func(string) []string
		}{
			{name: "drift", args: func(bridge string) []string {
				return []string{"link", "set", "dev", bridge, "address", "02:ff:ff:ff:ff:ff"}
			}},
			{name: "recreation", args: func(bridge string) []string {
				return []string{"link", "delete", "dev", bridge}
			}},
		} {
			By("checking router MAC recovery after bridge " + stage.name)
			routers, err := openperouter.Get(cs, true)
			Expect(err).To(Succeed())
			for origin := range routers.GetExecutors() {
				for _, vni := range vnis {
					bridge := fmt.Sprintf("br-pe-%d", vni.Spec.VNI)
					_, err := origin.Exec("ip", stage.args(bridge)...)
					Expect(err).To(Succeed())
				}
				// Restart guarantees reconciliation even without a Kubernetes resource change.
				Expect(systemd.RestartSystemdUnit(executor.ForNode(origin.NodeName()), "controller.service")).To(Succeed())
			}
			checkMACs()
		}
	})
})

func writeRouterMACNodeConfig(exec executor.Executor, path, content string) error {
	_, err := exec.Exec("sh", "-c", `printf '%s' "$1" > "$2"`, "sh", content, path)
	return err
}
