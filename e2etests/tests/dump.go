// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/frr"
	"github.com/openperouter/openperouter/e2etests/pkg/frrk8s"
	"github.com/openperouter/openperouter/e2etests/pkg/infra"
	"github.com/openperouter/openperouter/e2etests/pkg/k8s"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
)

func dumpIfFails(cs clientset.Interface) {
	if ginkgo.CurrentSpecReport().Failed() {
		dumpBGPInfo(ReportPath, ginkgo.CurrentSpecReport().LeafNodeText, cs, infra.LeafA, infra.LeafB, infra.KindLeaf)
		k8s.DumpInfo(K8sReporter, ginkgo.CurrentSpecReport().LeafNodeText)
	}
}

func dumpBGPInfo(basePath, testName string, cs clientset.Interface, clabContainers ...string) {
	testPath := path.Join(basePath, strings.ReplaceAll(testName, " ", "-"))
	err := os.Mkdir(testPath, 0755)
	if err != nil && !errors.Is(err, os.ErrExist) {
		ginkgo.GinkgoWriter.Printf("failed to create test dir: %s", err)
		return
	}

	executors := map[string]executor.Executor{}
	for _, c := range clabContainers {
		exec := executor.ForContainer(c)
		executors[c] = exec
	}

	routers, err := openperouter.Get(cs, HostMode)
	Expect(err).NotTo(HaveOccurred())
	routers.Dump(ginkgo.GinkgoWriter)

	for router := range routers.GetExecutors() {
		executors[router.Name()] = router
	}

	frrk8sPods, err := frrk8s.Pods(cs)
	Expect(err).NotTo(HaveOccurred())
	DumpPods("frrk8s", frrk8sPods)
	for _, pod := range frrk8sPods {
		podExec := executor.ForPod(pod.Namespace, pod.Name, "frr")
		executors[pod.Name] = podExec
	}

	for name, exec := range executors {
		dump, err := frr.RawDump(exec)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("External frr dump for %s failed %v", name, err)
			continue
		}
		f, err := logFileFor(testPath, fmt.Sprintf("frrdump-%s", name))
		if err != nil {
			ginkgo.GinkgoWriter.Printf("External frr dump for container %s, failed to open file %v", name, err)
			continue
		}
		fmt.Fprintf(f, "Dumping information for %s", name)
		_, err = fmt.Fprint(f, dump)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("External frr dump for container %s, failed to write to file %v", name, err)
			continue
		}
	}
}

func logFileFor(base string, kind string) (*os.File, error) {
	path := path.Join(base, kind) + ".log"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func DumpPods(name string, pods []*corev1.Pod) {
	ginkgo.GinkgoWriter.Printf("%s pods are:", name)
	for _, pod := range pods {
		ginkgo.GinkgoWriter.Printf("Pod %s/%s: %s", pod.Namespace, pod.Name, pod.Status.Phase)
		ginkgo.GinkgoWriter.Printf("  Node: %s", pod.Spec.NodeName)
		ginkgo.GinkgoWriter.Printf("  IPs: %v", pod.Status.PodIPs)
		ginkgo.GinkgoWriter.Printf("  Containers:")
		for _, c := range pod.Spec.Containers {
			ginkgo.GinkgoWriter.Printf("    - %s: %s", c.Name, c.Image)
		}
		ginkgo.GinkgoWriter.Print("\n")
	}
}
