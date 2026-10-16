// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/ipfamily"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const overlayDataPlaneTimeout = 2 * time.Minute

// overlayEndpoint is one side of an east-west overlay check: the workload pod and
// the node whose router namespace has to learn the peer's MAC.
type overlayEndpoint struct {
	node *corev1.Node
	pod  *corev1.Pod
}

// waitForUnderlaySessions blocks until every node has an established session with
// both leaves. Nodes are dual homed and a peer VTEP /32 may be learned over either
// uplink, so gating on a single leaf leaves half of the underlay unchecked.
func waitForUnderlaySessions(nodes []corev1.Node) {
	GinkgoHelper()

	for _, leaf := range []string{infra.KindLeaf, infra.KindLeaf2} {
		leafExec := executor.ForContainer(leaf)
		for _, node := range nodes {
			neighborIP, err := infra.NeighborIP(leaf, node.Name)
			Expect(err).To(Succeed())
			validate.SessionWithNeighbor(
				leafExec,
				validate.SessionParameters{
					FromName:    leaf,
					ToName:      node.Name,
					NeighborIP:  neighborIP,
					Established: Established,
				},
			)
		}
	}
}

// waitForL2OverlayDataPlane blocks until every endpoint has each peer's MAC
// programmed in its local vni<vni> FDB with the peer's VTEP as destination.
//
// An established BGP session only proves the control plane channel is up: zebra
// still has to install the type-2 route into the kernel FDB before the first
// east-west frame can be forwarded. On a router namespace rebuilt moments earlier
// that gap is wide enough for a whole reachability check to time out against a
// half-programmed data plane, which reads as a flake instead of as the missing
// FDB entry it is.
func waitForL2OverlayDataPlane(cs clientset.Interface, vni int32, endpoints ...overlayEndpoint) {
	GinkgoHelper()

	routers, err := openperouter.Get(cs, HostMode)
	Expect(err).To(Succeed())

	peers := make([]overlayPeer, 0, len(endpoints))
	for _, endpoint := range endpoints {
		peer, err := resolveOverlayPeer(routers, endpoint)
		Expect(err).To(Succeed())
		peers = append(peers, peer)
	}

	device := fmt.Sprintf("vni%d", vni)
	for _, local := range peers {
		for _, remote := range peers {
			if local.nodeName == remote.nodeName {
				continue
			}
			By(fmt.Sprintf("waiting for %s on %s to learn %s (%s) behind VTEP %s",
				device, local.nodeName, remote.podName, remote.mac, remote.vtep))
			Eventually(func(g Gomega) {
				fdb, err := local.router.Exec("bridge", "fdb", "show", "dev", device)
				g.Expect(err).To(Succeed())
				g.Expect(hasRemoteFDBEntry(fdb, remote.mac, remote.vtep)).To(BeTrue(),
					"%s on %s has no entry for %s via VTEP %s:\n%s",
					device, local.nodeName, remote.mac, remote.vtep, fdb)
			}).WithTimeout(overlayDataPlaneTimeout).WithPolling(time.Second).Should(Succeed())
		}
	}
}

// overlayPeer is an endpoint with everything the FDB assertion needs resolved:
// the MAC the peers have to learn, the VTEP it has to sit behind, and an executor
// into the router namespace that owns the bridge.
type overlayPeer struct {
	nodeName string
	podName  string
	mac      string
	vtep     string
	router   openperouter.RouterExecutor
}

func resolveOverlayPeer(routers openperouter.Routers, endpoint overlayEndpoint) (overlayPeer, error) {
	router, err := routers.ExecutorForNode(endpoint.node.Name)
	if err != nil {
		return overlayPeer{}, fmt.Errorf("failed to get the router on node %s: %w", endpoint.node.Name, err)
	}

	mac, err := podMAC(endpoint.pod, "net1")
	if err != nil {
		return overlayPeer{}, err
	}

	vtepCIDR, err := openperouter.GetVtepIPv4ForNode(infra.Underlay(infra.Default).Spec.TunnelEndpoint, endpoint.node)
	if err != nil {
		return overlayPeer{}, fmt.Errorf("failed to get the VTEP of node %s: %w", endpoint.node.Name, err)
	}

	return overlayPeer{
		nodeName: endpoint.node.Name,
		podName:  endpoint.pod.Name,
		mac:      mac,
		vtep:     ipfamily.StripCIDRMask(vtepCIDR),
		router:   router,
	}, nil
}

func podMAC(pod *corev1.Pod, device string) (string, error) {
	podExec := executor.ForPod(pod.Namespace, pod.Name, "agnhost")
	out, err := podExec.Exec("ip", "-o", "link", "show", "dev", device)
	if err != nil {
		return "", fmt.Errorf("failed to read %s in pod %s/%s: %s: %w", device, pod.Namespace, pod.Name, out, err)
	}

	fields := strings.Fields(out)
	for i, field := range fields {
		if field == "link/ether" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no hardware address for %s in pod %s/%s: %s", device, pod.Namespace, pod.Name, out)
}

// hasRemoteFDBEntry reports whether the FDB holds mac with vtep as its VXLAN
// destination. A bare mac entry is not enough: without a "dst" the kernel has
// nowhere to send the frame.
func hasRemoteFDBEntry(fdb, mac, vtep string) bool {
	for line := range strings.SplitSeq(fdb, "\n") {
		if strings.Contains(line, mac) && strings.Contains(line, "dst "+vtep) {
			return true
		}
	}
	return false
}
