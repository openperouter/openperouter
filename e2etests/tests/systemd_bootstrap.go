// SPDX-License-Identifier:Apache-2.0

package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/k8sclient"
	"github.com/openperouter/openperouter/e2etests/pkg/systemd"
	"github.com/openperouter/openperouter/internal/bootstrap"
	"sigs.k8s.io/yaml"
)

var BootstrapNode systemd.RebootNode

const bootstrapConfigPath = "/etc/openperouter/openpe_boot.yaml"

var _ = ginkgo.Describe("Systemd bootstrap reboot", ginkgo.Label("systemdmode"), func() {
	ginkgo.AfterEach(func() {
		dumpIfFails(k8sclient.New())
	})

	ginkgo.It("applies a changed log level after reboot", ginkgo.Serial, func() {
		content, err := BootstrapNode.Exec("cat", bootstrapConfigPath)
		gomega.Expect(err).To(gomega.Succeed())
		input, err := parseBootstrapConfig(content)
		gomega.Expect(err).To(gomega.Succeed())
		originalLogLevel := input.LogLevel
		logLevel := changedLogLevel(originalLogLevel)
		input.LogLevel = logLevel

		ginkgo.DeferCleanup(func() {
			gomega.Eventually(func() error {
				_, err := executor.ForNode(BootstrapNode.Name).Exec("true")
				return err
			}, 3*time.Minute, time.Second).Should(gomega.Succeed(), "node-exec helper must recover before subsequent tests")
		})

		ginkgo.DeferCleanup(func() {
			ginkgo.By("restoring logLevel and restarting the kind node")
			input.LogLevel = originalLogLevel
			gomega.Eventually(func() error { return writeBootstrap(input) },
				2*time.Minute, time.Second).Should(gomega.Succeed())
			gomega.Expect(BootstrapNode.Reboot()).To(gomega.Succeed())
			restoredLogLevel := "info"
			if originalLogLevel != "" {
				restoredLogLevel = originalLogLevel
			}
			gomega.Eventually(controllerLogLevel, 3*time.Minute, time.Second).Should(gomega.Equal(restoredLogLevel))
		})

		ginkgo.By("changing logLevel and restarting the kind node")
		gomega.Expect(writeBootstrap(input)).To(gomega.Succeed())
		gomega.Expect(BootstrapNode.Reboot()).To(gomega.Succeed())

		ginkgo.By("waiting for the controller container to appear")
		gomega.Eventually(func() error {
			_, err := BootstrapNode.Exec("podman", "container", "exists", "controller")
			return err
		}, 3*time.Minute, time.Second).Should(gomega.Succeed())

		gomega.Eventually(controllerLogLevel, 3*time.Minute, time.Second).Should(gomega.Equal(logLevel))
	})
})

func changedLogLevel(current string) string {
	if current == "debug" {
		return "info"
	}
	return "debug"
}

func controllerLogLevel() (string, error) {
	output, err := BootstrapNode.Exec("podman", "inspect", "controller", "--format", "{{json .}}")
	if err != nil {
		return "", err
	}
	var controller struct {
		Config struct{ Cmd []string }
		State  struct{ Running bool }
	}
	if err := json.Unmarshal([]byte(output), &controller); err != nil {
		return "", fmt.Errorf("parsing controller container: %w", err)
	}
	if !controller.State.Running {
		return "", fmt.Errorf("controller container is not running")
	}
	for _, arg := range controller.Config.Cmd {
		if level, ok := strings.CutPrefix(arg, "--loglevel="); ok {
			return level, nil
		}
	}
	return "", fmt.Errorf("controller log level argument is missing")
}

func parseBootstrapConfig(content string) (bootstrap.Deployment, error) {
	var input bootstrap.Deployment
	if err := yaml.UnmarshalStrict([]byte(content), &input); err != nil {
		return bootstrap.Deployment{}, err
	}
	return input, nil
}

func writeBootstrap(input bootstrap.Deployment) error {
	content, err := yaml.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshaling bootstrap configuration: %w", err)
	}
	_, err = BootstrapNode.Exec("sh", "-c",
		`set -eu
umask 077
tmp=$(mktemp /etc/openperouter/.bootstrap.XXXXXX)
trap 'rm -f "$tmp"' EXIT
printf %s "$1" > "$tmp"
mv "$tmp" "$2"`,
		"sh", string(content), bootstrapConfigPath)
	return err
}
