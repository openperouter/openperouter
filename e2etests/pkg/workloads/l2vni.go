// SPDX-License-Identifier:Apache-2.0

package workloads

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	clientset "k8s.io/client-go/kubernetes"
)

// L2VNIPod is a pod attached to an L2VNI through a macvlan NAD. The NAD master
// is a workload VF when the L2VNI rides a trunk, or the managed host bridge of
// a plain L2VNI.
type L2VNIPod struct {
	Name      string
	VNI       v1alpha1.L2VNI
	IP        string
	NADMaster string
}

// HostBridgeName mirrors the name the controller gives to the managed host
// bridge of an L2VNI.
func HostBridgeName(vni int32) string {
	return fmt.Sprintf("br-hs-%d", vni)
}

// CreateL2VNIPods creates one macvlan NAD and one agnhost pod per entry, all
// on the given node, and returns the pods' executors keyed by pod name.
func CreateL2VNIPods(cs clientset.Interface, namespace, nodeName string, pods []L2VNIPod) map[string]executor.Executor {
	GinkgoHelper()
	executors := map[string]executor.Executor{}
	for _, pod := range pods {
		By(fmt.Sprintf("creating pod %s in VNI %d", pod.Name, pod.VNI.Spec.VNI))
		nad, err := k8s.CreateMacvlanNad(pod.Name, namespace, pod.NADMaster, pod.VNI.Spec.GatewayIPs)
		Expect(err).To(Succeed())
		created, err := k8s.CreateAgnhostPod(cs, pod.Name, namespace,
			k8s.WithNad(nad.Name, namespace, []string{pod.IP}),
			k8s.OnNode(nodeName),
		)
		Expect(err).To(Succeed())
		executors[pod.Name] = executor.ForPod(created.Namespace, created.Name, "agnhost")
	}
	return executors
}

func CanPingFromPod(exec executor.Executor, ip string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		out, err := exec.Exec("ping", "-c", "1", "-W", "2", "-I", "net1", ip)
		g.Expect(err).To(Succeed(), "ping to %s failed: %s", ip, out)
	}).WithTimeout(40 * time.Second).WithPolling(time.Second).Should(Succeed())
}

func AddressOnly(cidr string) string {
	return strings.Split(cidr, "/")[0]
}
