// SPDX-License-Identifier:Apache-2.0

package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/onsi/ginkgo/v2"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openshift-kni/k8sreporter"
	"k8s.io/apimachinery/pkg/runtime"
)

const inspectTimeout = 5 * time.Minute
const hostInspectTimeout = 5 * time.Minute
const hostCopyTimeout = time.Minute
const nodeListTimeout = 30 * time.Second
const remoteCleanupTimeout = 30 * time.Second

// InspectReporter invokes the inspect tool for failed e2e specs.
type InspectReporter struct {
	inspectPath          string
	inspectHostPath      string
	reportPath           string
	k8sClient            string
	containerRuntime     string
	namespace            string
	additionalNamespaces []string
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

// WithHostInspection runs inspect_host on every node and copies its artifacts into the report.
func WithHostInspection(inspectHostPath, containerRuntime string) func(*InspectReporter) {
	return func(reporter *InspectReporter) {
		reporter.inspectHostPath = inspectHostPath
		reporter.containerRuntime = containerRuntime
	}
}

// Dump invokes the repository inspect tool and stores its output with the artifacts for the failed spec.
func (r *InspectReporter) Dump(testName string, testNamespaces ...string) {
	outputPath := filepath.Join(r.reportPath, sanitizeTestName(testName))
	r.dumpKubernetesInfo(outputPath, testNamespaces)
	if r.inspectHostPath != "" || r.containerRuntime != "" {
		r.dumpHostInfo(outputPath)
	}
}

func (r *InspectReporter) dumpKubernetesInfo(outputPath string, testNamespaces []string) {
	args := r.inspectArgs(outputPath, testNamespaces)
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

func (r *InspectReporter) dumpHostInfo(outputPath string) {
	if r.inspectHostPath == "" || r.containerRuntime == "" {
		ginkgo.GinkgoWriter.Printf(
			"host inspection skipped: inspectHostPath=%q containerRuntime=%q\n", r.inspectHostPath, r.containerRuntime)
		return
	}

	script, err := os.ReadFile(r.inspectHostPath)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("host inspection failed to read %s: %v\n", r.inspectHostPath, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), nodeListTimeout)
	defer cancel()
	nodes, err := r.nodeNames(ctx)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("host inspection failed to list nodes: %v\n", err)
		return
	}

	for _, node := range nodes {
		if err := r.dumpHostNode(script, node, outputPath); err != nil {
			ginkgo.GinkgoWriter.Printf("host inspection failed for node %s: %v\n", node, err)
		}
	}
	ginkgo.GinkgoWriter.Printf("Host inspection completed for %d node(s).\n", len(nodes))
}

func (r *InspectReporter) dumpHostNode(script []byte, nodeName, outputPath string) error {
	localPath, err := hostInfoPath(outputPath, nodeName)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(localPath); err != nil {
		return fmt.Errorf("removing existing host artifacts at %s: %w", localPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("creating host artifact parent directory %s: %w", filepath.Dir(localPath), err)
	}

	remotePath := fmt.Sprintf("/var/tmp/openperouter-inspect-host-%d", time.Now().UnixNano())
	defer func() {
		if err := r.removeRemoteArtifacts(nodeName, remotePath); err != nil {
			ginkgo.GinkgoWriter.Printf("host inspection cleanup failed for node %s: %v\n", nodeName, err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), hostInspectTimeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		r.containerRuntime,
		"exec", "-i", nodeName,
		"env", "ARTIFACTS_DIR="+remotePath,
		"bash",
	)
	cmd.Stdin = bytes.NewReader(script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = time.Second
	output, execErr := cmd.CombinedOutput()

	copyCtx, copyCancel := context.WithTimeout(context.Background(), hostCopyTimeout)
	defer copyCancel()
	copyOutput, copyErr := exec.CommandContext(
		copyCtx,
		r.containerRuntime,
		"cp", nodeName+":"+remotePath, localPath,
	).CombinedOutput()

	if execErr != nil && copyErr != nil {
		return fmt.Errorf(
			"executing inspect_host: %w\n%s\ncopying artifacts also failed: %w\n%s",
			execErr, output, copyErr, copyOutput,
		)
	}
	if execErr != nil {
		return fmt.Errorf("executing inspect_host: %w\n%s", execErr, output)
	}
	if copyErr != nil {
		return fmt.Errorf("copying %s:%s to %s: %w\n%s\nscript output:\n%s", nodeName, remotePath, localPath, copyErr, copyOutput, output)
	}
	return nil
}

func (r *InspectReporter) removeRemoteArtifacts(nodeName, remotePath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteCleanupTimeout)
	defer cancel()

	output, err := exec.CommandContext(
		ctx,
		r.containerRuntime,
		"exec", nodeName, "rm", "-rf", "--", remotePath,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("removing %s:%s: %w\n%s", nodeName, remotePath, err, output)
	}
	return nil
}

func (r *InspectReporter) nodeNames(ctx context.Context) ([]string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.k8sClient, "get", "nodes", "-o", "json")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("getting nodes with %s: %w\n%s", r.k8sClient, err, stderr.String())
	}

	var nodeList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &nodeList); err != nil {
		return nil, fmt.Errorf("parsing node list: %w", err)
	}

	nodes := make([]string, 0, len(nodeList.Items))
	for _, item := range nodeList.Items {
		if item.Metadata.Name != "" {
			nodes = append(nodes, item.Metadata.Name)
		}
	}
	return nodes, nil
}

func (r *InspectReporter) inspectArgs(outputPath string, testNamespaces []string) []string {
	args := []string{
		"--k8s-client=" + r.k8sClient,
		"--dest-dir=" + outputPath,
		"--namespace=" + r.namespace,
		"--since=10m",
	}
	for _, namespace := range r.additionalNamespaces {
		args = append(args, "--additional-namespace="+namespace)
	}
	for _, namespace := range testNamespaces {
		args = append(args, "--additional-namespace="+namespace)
	}
	return args
}

func hostInfoPath(outputPath, nodeName string) (string, error) {
	if nodeName == "" || strings.ContainsAny(nodeName, `/\`) || nodeName == "." || nodeName == ".." {
		return "", fmt.Errorf("invalid node name %q", nodeName)
	}
	return filepath.Join(outputPath, "HOST_INFO", nodeName), nil
}

func sanitizeTestName(testName string) string {
	nonAlphanumeric := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	return nonAlphanumeric.ReplaceAllString(testName, "_")
}
