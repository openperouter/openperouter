// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/e2etests/pkg/config"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	"github.com/openperouter/openperouter/e2etests/pkg/validate"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

// When zebra dies the other daemons keep running, and the stack must come back
// whole without a container restart: every daemon watchfrr supervises restarted
// under a new pid, zebra mapping the VRF to its L3VNI, the session to the leaf
// Established, the pod Ready.
var _ = Describe("FRR daemon death", PodModeOnly, Ordered, func() {
	var cs clientset.Interface

	vniRed := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "red", Namespace: openperouter.Namespace},
		Spec:       v1alpha1.L3VNISpec{VRF: "red", VNI: 100},
	}

	BeforeAll(func() {
		cs = k8sclient.New()
		Eventually(func() error {
			routers, err := openperouter.Get(cs, HostMode)
			if err != nil {
				return err
			}
			return openperouter.AreReady(routers)
		}, 2*time.Minute, time.Second).ShouldNot(HaveOccurred())

		Expect(Updater.Update(config.Resources{
			Underlays: []v1alpha1.Underlay{infra.Underlay},
			L3VNIs:    []v1alpha1.L3VNI{vniRed},
		})).To(Succeed())

		DeferCleanup(func() {
			Expect(Updater.CleanAll()).To(Succeed())
		})
	})

	It("heals in place after zebra dies", func() {
		const killed = "zebra"
		routerPods, err := openperouter.RouterPodsForNodes(cs, allNodes(cs))
		Expect(err).NotTo(HaveOccurred())
		Expect(routerPods).NotTo(BeEmpty())
		routerPod := routerPods[0]
		nodeName := routerPod.Spec.NodeName
		frrExec := executor.ForPod(openperouter.Namespace, routerPod.Name, "frr")

		daemons, err := frrDaemonNames(frrExec)
		Expect(err).NotTo(HaveOccurred())
		Expect(daemons).To(ContainElement(killed), "the daemons watchfrr supervises must include %s", killed)

		By("waiting for zebra to map red to VNI 100 before the kill")
		Eventually(func(g Gomega) {
			g.Expect(frrExec.Exec("vtysh", "-c", "show vrf vni json")).To(
				SatisfyAll(ContainSubstring(`"vrf":"red"`), ContainSubstring(`"vni":100`)))
		}, 2*time.Minute, time.Second).Should(Succeed())

		By("recording the pid of every daemon watchfrr supervises: " + strings.Join(daemons, " "))
		pidsBefore, err := pidsForProcesses(frrExec, daemons)
		Expect(err).NotTo(HaveOccurred())
		for _, d := range daemons {
			Expect(pidsBefore[d]).To(HaveLen(1),
				"expected exactly one live %s on %s, got %v", d, nodeName, pidsBefore[d])
		}

		By("killing " + killed + " with SIGKILL in the router pod")
		Expect(frrExec.Exec("kill", "-9", pidsBefore[killed][0])).Error().ToNot(HaveOccurred())

		By("waiting for the whole stack to come back")
		Eventually(func(g Gomega) {
			pidsNow, err := pidsForProcesses(frrExec, daemons)
			g.Expect(err).NotTo(HaveOccurred())

			// Every supervised daemon must have been restarted, not just the one
			// that was killed: the surviving daemons never replay their state into
			// a new zebra, so a stack that comes back with only zebra respawned is
			// the regression this spec exists to catch.
			for _, d := range daemons {
				g.Expect(pidsNow[d]).To(HaveLen(1), "%s is not running again", d)
				g.Expect(pidsNow[d][0]).NotTo(Equal(pidsBefore[d][0]), "%s was not restarted with the stack", d)
			}

			g.Expect(frrExec.Exec("vtysh", "-c", "show vrf vni json")).To(
				SatisfyAll(
					ContainSubstring(`"vrf":"red"`),
					ContainSubstring(`"vni":100`)),
			)
			g.Expect(frrExec.Exec("vtysh", "-c", "show evpn vni 100")).To(ContainSubstring("Type: L3"))
			routers, err := openperouter.Get(cs, HostMode)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(openperouter.AreReady(routers)).To(Succeed())
		}, 4*time.Minute, 2*time.Second).Should(Succeed())

		neighborIP, err := infra.NeighborIP(infra.KindLeaf, nodeName)
		Expect(err).NotTo(HaveOccurred())
		validate.SessionWithNeighbor(
			executor.ForContainer(infra.KindLeaf),
			validate.SessionParameters{
				FromName:    infra.KindLeaf,
				ToName:      nodeName,
				NeighborIP:  neighborIP,
				Established: Established,
			},
		)
	})
})

// watchfrrName is the supervisor, which frr_names deliberately leaves out: it
// restarts the daemons rather than being restarted with them.
const watchfrrName = "watchfrr"

// frrDaemonNames returns the daemons watchfrr supervises so the spec covers
// whatever daemon_list enables rather than a list that silently drifts from
// the chart's daemons.
func frrDaemonNames(frrExec executor.Executor) ([]string, error) {
	out, err := frrExec.Exec("vtysh", "-c", "show daemons")
	if err != nil {
		return nil, fmt.Errorf("listing the frr daemons failed: %w (output: %s)", err, out)
	}
	names := strings.Fields(strings.ReplaceAll(out, watchfrrName, ""))
	if len(names) == 0 {
		return nil, fmt.Errorf("frr_names reported no daemons (output: %q)", out)
	}
	return names, nil
}

// pidsForProcesses maps each named process to the pids the router pod sees for
// it. ps runs in the target container because the test process cannot read the
// pod's /proc namespace directly.
func pidsForProcesses(executor executor.Executor, processNames []string) (map[string][]string, error) {
	out, err := executor.Exec("ps", "-o", "pid,comm,stat")
	if err != nil {
		return nil, fmt.Errorf("listing the pids of %v in the router pod failed: %w (output: %s)", processNames, err, out)
	}

	wanted := make(map[string]struct{}, len(processNames))
	for _, name := range processNames {
		wanted[name] = struct{}{}
	}

	pids := make(map[string][]string, len(processNames))
	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		state := fields[len(fields)-1]
		if strings.HasPrefix(state, "Z") {
			continue
		}

		name := strings.Join(fields[1:len(fields)-1], " ")
		if _, ok := wanted[name]; ok {
			pids[name] = append(pids[name], strconv.Itoa(pid))
		}
	}
	return pids, nil
}
