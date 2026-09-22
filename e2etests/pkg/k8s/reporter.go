// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openshift-kni/k8sreporter"
	"k8s.io/apimachinery/pkg/runtime"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func InitReporter(kubeconfig, path string, namespaces ...string) (*k8sreporter.KubernetesReporter, error) {
	addToScheme := func(s *runtime.Scheme) error {
		if err := v1alpha1.AddToScheme(s); err != nil {
			return err
		}
		if err := frrk8sv1beta1.AddToScheme(s); err != nil {
			return err
		}
		return nil
	}

	dumpNamespace := func(ns string) bool {
		return slices.Contains(namespaces, ns)
	}

	crds := []k8sreporter.CRData{
		{Cr: &v1alpha1.UnderlayList{}},
		{Cr: &v1alpha1.L3PassthroughList{}},
		{Cr: &v1alpha1.L3VNIList{}},
		{Cr: &v1alpha1.L2VNIList{}},
		{Cr: &v1alpha1.L3VPNList{}},
		{Cr: &v1alpha1.RawFRRConfigList{}},
		{Cr: &v1alpha1.RouterNodeConfigurationStatusList{}},
		{Cr: &frrk8sv1beta1.FRRConfigurationList{}},
	}

	reporter, err := k8sreporter.New(kubeconfig, addToScheme, dumpNamespace, path, crds...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize k8s reporter: %w", err)
	}
	return reporter, nil
}

// DumpReporterInfo collects diagnostics with the legacy k8sreporter collector.
func DumpReporterInfo(reporter *k8sreporter.KubernetesReporter, testName string) {
	testNameNoSpaces := nonAlphanumeric.ReplaceAllString(testName, "_")
	reporter.Dump(10*time.Minute, testNameNoSpaces)
}

// DumpInfo invokes the repository inspect tool and stores its output with the
// artifacts for the failed spec.
func DumpInfo(reportPath, testName, kubectl string, namespaces ...string) {
	inspect, err := inspectPath()
	if err != nil {
		ginkgo.GinkgoWriter.Printf("failed to locate inspect tool: %v", err)
		return
	}

	outputPath := filepath.Join(reportPath, nonAlphanumeric.ReplaceAllString(testName, "_"))
	args := []string{
		"--k8s-client=" + kubectl,
		"--dest-dir=" + outputPath,
		"--since=10m",
	}
	for _, namespace := range namespaces {
		args = append(args, "--namespace="+namespace)
	}

	cmd := exec.Command(inspect, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		ginkgo.GinkgoWriter.Printf("inspect failed: %v\n%s", err, output)
		return
	}

	if os.Getenv("GITHUB_ACTIONS") == "true" {
		ginkgo.GinkgoWriter.Printf(
			"Inspect diagnostics are in the job's kind-logs artifact under %s/",
			filepath.Base(outputPath),
		)
		return
	}

	ginkgo.GinkgoWriter.Printf("Inspect completed. Artifacts are stored in %s", outputPath)
}

func inspectPath() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, "tools", "inspect", "inspect")
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("tools/inspect/inspect not found from %s", dir)
		}
		dir = parent
	}
}
