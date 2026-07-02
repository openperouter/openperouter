// SPDX-License-Identifier:Apache-2.0

package systemd

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

// RestartSystemdUnit restarts the systemd unit on the node and waits for it
// to become active again with a fresh main PID.
func RestartSystemdUnit(exec executor.Executor, unit string) error {
	pidBefore, err := exec.Exec("systemctl", "show", "--property=MainPID", "--value", unit)
	if err != nil {
		return err
	}
	pidBefore = strings.TrimSpace(pidBefore)
	_, err = exec.Exec("systemctl", "restart", unit)
	if err != nil {
		return err
	}
	oneMinuteTimeout := wait.Backoff{
		Steps:    60,
		Duration: time.Second,
	}
	allErrors := func(error) bool {
		return true
	}
	return retry.OnError(oneMinuteTimeout, allErrors, func() error {
		output, err := exec.Exec("systemctl", "is-active", unit)
		if err != nil {
			return err
		}
		if strings.TrimSpace(output) != "active" {
			return fmt.Errorf("unit %s is not active: %s", unit, output)
		}
		pidAfter, err := exec.Exec("systemctl", "show", "--property=MainPID", "--value", unit)
		if err != nil {
			return err
		}
		if strings.TrimSpace(pidAfter) == pidBefore {
			return fmt.Errorf("unit %s PID has not changed, still %s", unit, pidBefore)
		}
		return nil
	})
}
