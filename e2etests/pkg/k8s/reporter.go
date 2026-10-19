// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"syscall"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openshift-kni/k8sreporter"
	"k8s.io/apimachinery/pkg/runtime"
)

const inspectTimeout = 5 * time.Minute

// InspectReporter invokes the inspect tool for failed e2e specs.
type InspectReporter struct {
	inspectPath           string
	reportPath            string
	k8sClient             string
	namespace             string
	additionalNamespaces  []string
	externalFRRContainers []string
}

func InitReporter(kubeconfig, path string, namespaces ...string) (*k8sreporter.KubernetesReporter, error) {
	// When using custom crds, we need to add them to the scheme
	addToScheme := func(s *runtime.Scheme) error {
		err := v1alpha1.AddToScheme(s)
		if err != nil {
			return err
		}
		err = frrk8sv1beta1.AddToScheme(s)
		if err != nil {
			return err
		}

		return nil
	}

	// The namespaces we want to dump resources for (including pods and pod logs)
	dumpNamespace := func(ns string) bool {
		return slices.Contains(namespaces, ns)
	}

	// The list of CRDs we want to dump
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

func DumpInfo(reporter *k8sreporter.KubernetesReporter, testName string) {
	reporter.Dump(10*time.Minute, sanitizeTestName(testName))
}

// NewInspectReporter creates an inspect collector for failed e2e specs.
func NewInspectReporter(
	inspectPath, reportPath, k8sClient, namespace string,
	options ...func(*InspectReporter),
) *InspectReporter {
	reporter := &InspectReporter{
		inspectPath: inspectPath,
		reportPath:  reportPath,
		k8sClient:   k8sClient,
		namespace:   namespace,
	}
	for _, option := range options {
		option(reporter)
	}
	return reporter
}

// WithAdditionalNamespaces adds namespaces whose resources and logs inspect collects.
func WithAdditionalNamespaces(namespaces ...string) func(*InspectReporter) {
	return func(reporter *InspectReporter) {
		reporter.additionalNamespaces = append(reporter.additionalNamespaces, namespaces...)
	}
}

// WithExternalFRRContainers adds external FRR containers inspected through the configured runtime.
func WithExternalFRRContainers(containers ...string) func(*InspectReporter) {
	return func(reporter *InspectReporter) {
		reporter.externalFRRContainers = append(reporter.externalFRRContainers, containers...)
	}
}

// Dump invokes the repository inspect tool and stores its output with the artifacts for the failed spec.
func (r *InspectReporter) Dump(testName string) {
	outputPath := filepath.Join(r.reportPath, sanitizeTestName(testName))
	args := r.inspectArgs(outputPath)

	ctx, cancel := context.WithTimeout(context.Background(), inspectTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.inspectPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		ginkgo.GinkgoWriter.Printf("inspect failed: %v\n%s", err, output)
		return
	}

	ginkgo.GinkgoWriter.Printf("Inspect completed. Artifacts are stored under %s/\n", filepath.Base(outputPath))
}

func (r *InspectReporter) inspectArgs(outputPath string) []string {
	args := []string{
		"--k8s-client=" + r.k8sClient,
		"--dest-dir=" + outputPath,
		"--namespace=" + r.namespace,
		"--since=10m",
	}
	for _, namespace := range r.additionalNamespaces {
		args = append(args, "--additional-namespace="+namespace)
	}
	for _, container := range r.externalFRRContainers {
		args = append(args, "--ext-frr-container="+container)
	}
	return args
}

func sanitizeTestName(testName string) string {
	nonAlphanumeric := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	return nonAlphanumeric.ReplaceAllString(testName, "_")
}
