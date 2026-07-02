// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

func dumpIfFails(cs clientset.Interface, additionalNamespaces ...string) {
	slices.Sort(additionalNamespaces)
	additionalNamespaces = slices.Compact(additionalNamespaces)

	if ginkgo.CurrentSpecReport().Failed() {
		dumpFRRInfo(ReportPath, ginkgo.CurrentSpecReport().FullText(), cs, GroutMode, infra.LeafA, infra.LeafB, infra.KindLeaf)
		for _, namespace := range additionalNamespaces {
			dumpWorkloadInfo(ReportPath, ginkgo.CurrentSpecReport().FullText(), cs, namespace)
		}
		k8s.DumpInfo(K8sReporter, ginkgo.CurrentSpecReport().FullText())
		if HostMode {
			dumpPodmanInfo(cs, ReportPath, ginkgo.CurrentSpecReport().FullText())
		}
	}
}

func dumpFRRInfo(basePath, testName string, cs clientset.Interface, groutMode bool, clabContainers ...string) {
	testPath, err := createTestOutput(basePath, testName)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpFRRInfo: failed to create test dir: %v", err)
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
		func() {
			dump := frr.RawDump(exec)
			if groutMode {
				groutDump := frr.GroutDump(exec)
				dump += "\n\n" + groutDump
			}

			f, err := logFileFor(testPath, fmt.Sprintf("frrdump-%s", name))
			if err != nil {
				ginkgo.GinkgoWriter.Printf("dumpFRRInfo: external frr dump for container %s, failed to open file %v", name, err)
				return
			}
			defer func() {
				if err := f.Close(); err != nil {
					ginkgo.GinkgoWriter.Printf("dumpFRRInfo: failed to close file %s, err: %v", f.Name(), err)
				}
			}()
			fmt.Fprintf(f, "Dumping information for %s", name)
			if _, err = fmt.Fprint(f, dump); err != nil {
				ginkgo.GinkgoWriter.Printf("dumpFRRInfo: external frr dump for container %s, failed to write to file %v", name, err)
				return
			}
		}()
	}
}

// dumpWorkloadInfo gathers the pod list inside namespace and stores it in a file, in YAML format.
// It also runs an executor inside each pod where it collects basic networking information.
func dumpWorkloadInfo(basePath, testName string, cs clientset.Interface, namespace string) {
	testPath, err := createTestOutput(basePath, testName)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpWorkloadInfo: failed to create test dir: %s", err)
		return
	}

	pods, err := cs.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpWorkloadInfo: failed to list pods in namespace %s: %v", namespace, err)
		return
	}
	dumpPodList(pods.Items, testPath, namespace)

	executors := map[string]executor.Executor{}
	for _, pod := range pods.Items {
		if len(pod.Spec.Containers) == 0 {
			continue
		}
		// All containers in a pod share the same network namespace.
		container := pod.Spec.Containers[0]
		exec := executor.ForPod(pod.Namespace, pod.Name, container.Name)
		executors[fmt.Sprintf("%s-%s", pod.Namespace, pod.Name)] = exec
	}

	for name, exec := range executors {
		func() {
			dump := podNetworkInfo(exec)
			f, err := logFileFor(testPath, fmt.Sprintf("pod-container-dump-%s", name))
			if err != nil {
				ginkgo.GinkgoWriter.Printf("dumpWorkloadInfo: external dump for pod container %s, failed to open file %v", name, err)
				return
			}
			defer func() {
				if err := f.Close(); err != nil {
					ginkgo.GinkgoWriter.Printf("dumpWorkloadInfo: failed to close file %s, err: %v", f.Name(), err)
				}
			}()
			fmt.Fprintf(f, "Dumping information for %s", name)
			if _, err = fmt.Fprint(f, dump); err != nil {
				ginkgo.GinkgoWriter.Printf("dumpWorkloadInfo: external dump for pod container %s, failed to write to file %v", name, err)
				return
			}
		}()
	}
}

func dumpPodList(pods []corev1.Pod, testPath, namespace string) {
	fileName := fmt.Sprintf("pod-list-namespace-%s", namespace)
	f, err := logFileFor(testPath, fileName)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodList: failed to open file %s for namespace %s: %v",
			fileName, namespace, err)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			ginkgo.GinkgoWriter.Printf("dumpPodList: failed to close file %s, err: %v", f.Name(), err)
		}
	}()
	fmt.Fprintf(f, "Dumping pod YAMLs for namespace %s\n", namespace)
	out, err := yaml.Marshal(pods)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodList: failed to marshal pods to yaml for namespace %s: %v",
			namespace, err)
		return
	}
	if _, err = fmt.Fprint(f, string(out)); err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodList: failed to write to file %s for namespace %s: %v",
			fileName, namespace, err)
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

func dumpPodmanInfo(cs clientset.Interface, basePath, testName string) {
	testPath, err := createTestOutput(basePath, testName)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodmanInfo: failed to create test dir: %s", err)
		return
	}

	nodes, err := k8s.GetNodes(cs)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodmanInfo: failed to get nodes: %v", err)
		return
	}

	dump, err := openperouter.DumpPodmanLogs(nodes)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodmanInfo: failed to dump podman logs: %v", err)
	}

	f, err := logFileFor(testPath, "podmandump")
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodmanInfo: failed to open file: %v", err)
		return
	}
	defer f.Close()

	fmt.Fprint(f, "Dumping podman information\n")
	_, err = fmt.Fprint(f, dump)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("dumpPodmanInfo: failed to write to file: %v", err)
		return
	}
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

func createTestOutput(basePath, testName string) (string, error) {
	nonAlphanumeric := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	sanitizedName := nonAlphanumeric.ReplaceAllString(testName, "_")
	testPath := path.Join(basePath, sanitizedName)
	err := os.Mkdir(testPath, 0755)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("failed to create test dir: %w", err)
	}
	return testPath, nil
}

func podNetworkInfo(exec executor.Executor) string {
	var res strings.Builder

	commands := []struct {
		desc string
		cmd  []string
	}{
		{"ip link", []string{"bash", "-c", "ip l"}},
		{"ip address", []string{"bash", "-c", "ip address"}},
		{"ip neigh", []string{"bash", "-c", "ip neigh"}},
		{"Detailed interface statistics", []string{"bash", "-c", "ip -s -s link ls"}},
		{"ip route table all", []string{"bash", "-c", "ip route show table all"}},
	}

	for _, c := range commands {
		fmt.Fprintf(&res, "\n######## %s\n\n", c.desc)
		out, err := exec.Exec(c.cmd[0], c.cmd[1:]...)
		if err != nil {
			fmt.Fprintf(&res, "\nFailed exec %q: %v", strings.Join(c.cmd, " "), err)
		}
		res.WriteString(out)
	}
	return res.String()
}
