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
)

var BootstrapNode string

var _ = ginkgo.Describe("Systemd bootstrap", ginkgo.Label("systemdmode"), func() {
	ginkgo.AfterEach(func() {
		dumpIfFails(k8sclient.New())
	})

	// The systemd lane sets debug in boot.yaml. Seeing debug on the running
	// controller confirms bootstrap applied the setting instead of using the info default.
	ginkgo.It("starts the controller with debug logging", func() {
		gomega.Eventually(controllerLogLevel, time.Minute, time.Second).Should(gomega.Equal("debug"))
	})
})

func controllerLogLevel() (string, error) {
	output, err := executor.ForContainer(BootstrapNode).Exec("podman", "inspect", "controller", "--format", "{{json .}}")
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
