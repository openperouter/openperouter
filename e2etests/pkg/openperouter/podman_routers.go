// SPDX-License-Identifier:Apache-2.0

package openperouter

import (
	"fmt"
	"io"
	"iter"
	"strings"

	"github.com/openperouter/openperouter/e2etests/pkg/executor"
)

type routerPodmans struct {
	routers []routerPodman
}

type routerPodman struct {
	nodeName string
	pid      string
}

func (r routerPodman) Exec(cmd string, args ...string) (string, error) {
	return executor.ForPodmanInContainer(r.nodeName, "frr").Exec(cmd, args...)
}

func (r routerPodman) Name() string {
	return r.nodeName
}

func (r routerPodmans) GetExecutors() iter.Seq[RouterExecutor] {
	return func(yield func(RouterExecutor) bool) {
		for _, router := range r.routers {
			if !yield(router) {
				return
			}
		}
	}
}

func (r routerPodmans) Dump(writer io.Writer) {
	fmt.Fprintf(writer, "router pods are")
	for _, router := range r.routers {
		fmt.Fprintf(writer, "  Node: %s", router.nodeName)
		fmt.Fprint(writer, "\n")
	}
}

func podmanRolled(oldRouters, newRouters routerPodmans) error {
	// Check same number of routers
	if len(newRouters.routers) != len(oldRouters.routers) {
		return fmt.Errorf("new routers len %d different from old routers len: %d", len(newRouters.routers), len(oldRouters.routers))
	}

	oldPIDs := make(map[string]string)
	for _, router := range oldRouters.routers {
		oldPIDs[router.nodeName] = router.pid
	}

	// Check that all PIDs have changed (indicating restart)
	for _, newRouter := range newRouters.routers {
		oldPID, exists := oldPIDs[newRouter.nodeName]
		if !exists {
			return fmt.Errorf("new router found on node %s that was not in old routers", newRouter.nodeName)
		}
		if newRouter.pid == oldPID {
			return fmt.Errorf("router on node %s has same PID %s (not restarted)", newRouter.nodeName, newRouter.pid)
		}

		// Check that all required FRR daemons are running
		if err := checkFRRDaemons(newRouter); err != nil {
			return fmt.Errorf("frr daemons check failed on node %s: %w", newRouter.nodeName, err)
		}
	}

	return nil
}

func checkFRRDaemons(router routerPodman) error {
	out, err := router.Exec("vtysh", "-c", "show daemons")
	if err != nil {
		fmt.Println("fede checking daemons failed", out, err)
		return fmt.Errorf("failed to run 'show daemons': %w, output: %s", err, out)
	}

	requiredDaemons := []string{"bfdd", "bgpd", "zebra"}
	for _, daemon := range requiredDaemons {
		if !strings.Contains(out, daemon) {
			return fmt.Errorf("daemon %s not found in output: %s", daemon, out)
		}
	}

	return nil
}

func getPodmanRouterPID(nodeName string) (string, error) {
	exec := executor.ForContainer(nodeName)
	// Read the PID from the file written by the router pod
	out, err := exec.Exec("cat", "/etc/perouter/frr/frr.pid")
	if err != nil {
		return "", fmt.Errorf("failed to read PID file: %w, output: %s", err, out)
	}
	pid := strings.TrimSpace(out)
	if pid == "" || pid == "0" {
		return "", fmt.Errorf("invalid PID in file: %s", pid)
	}
	return pid, nil
}
