// SPDX-License-Identifier:Apache-2.0

package executor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	utilrand "k8s.io/apimachinery/pkg/util/rand"
)

var Kubectl string

type Executor interface {
	Exec(cmd string, args ...string) (string, error)
}

type hostExecutor struct{}

var (
	Host             hostExecutor
	ContainerRuntime = "docker"
)

func init() {
	if cr := os.Getenv("CONTAINER_RUNTIME"); len(cr) != 0 {
		ContainerRuntime = cr
	}
}

func (hostExecutor) Exec(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	return string(out), err
}

func ForContainer(containerName string) Executor {
	return &containerExecutor{container: containerName}
}

type containerExecutor struct {
	container string
}

func (e *containerExecutor) Exec(cmd string, args ...string) (string, error) {
	newArgs := append([]string{"exec", e.container, cmd}, args...)
	out, err := exec.Command(ContainerRuntime, newArgs...).CombinedOutput()
	return string(out), err
}

type podmanInContainerExecutor struct {
	outerContainer string
	innerContainer string
}

func ForPodmanInContainer(outerContainer, innerContainer string) Executor {
	return &podmanInContainerExecutor{
		outerContainer: outerContainer,
		innerContainer: innerContainer,
	}
}

func (e *podmanInContainerExecutor) Exec(cmd string, args ...string) (string, error) {
	// Build: docker/podman exec <outerContainer> podman exec <innerContainer> <cmd> <args...>
	newArgs := append([]string{"exec", e.outerContainer, "podman", "exec", e.innerContainer, cmd}, args...)
	out, err := exec.Command(ContainerRuntime, newArgs...).CombinedOutput()
	return string(out), err
}

type podExecutor struct {
	namespace string
	name      string
	container string
}

func ForPod(namespace, name, container string) Executor {
	return &podExecutor{
		namespace: namespace,
		name:      name,
		container: container,
	}
}

func (p *podExecutor) Exec(cmd string, args ...string) (string, error) {
	if Kubectl == "" {
		return "", errors.New("the kubectl parameter is not set")
	}
	fullargs := append([]string{"exec", p.name, "-n", p.namespace, "-c", p.container, "--", cmd}, args...)
	out, err := exec.Command(Kubectl, fullargs...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("exec in pod %s/%s container %s failed: %w. Output: %s",
			p.namespace, p.name, p.container, err, string(out))
	}
	return string(out), nil
}

// ForPodInNamedNetns returns an executor that runs commands inside a pod container
// but within a specific network namespace via nsenter. This is needed when the
// pod's processes use nsenter to run in a named netns (e.g. /var/run/netns/perouter),
// because kubectl exec enters the pod's own network namespace, not the named one.
func ForPodInNamedNetns(namespace, name, container, netnsPath string) Executor {
	return &podNetnsExecutor{
		namespace: namespace,
		name:      name,
		container: container,
		netnsPath: netnsPath,
	}
}

type podNetnsExecutor struct {
	namespace string
	name      string
	container string
	netnsPath string
}

func (p *podNetnsExecutor) Exec(cmd string, args ...string) (string, error) {
	if Kubectl == "" {
		return "", errors.New("the kubectl parameter is not set")
	}
	nsenterArgs := []string{"exec", p.name, "-n", p.namespace, "-c", p.container, "--",
		"nsenter", "--net=" + p.netnsPath, cmd}
	fullargs := append(nsenterArgs, args...)
	out, err := exec.Command(Kubectl, fullargs...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("exec nsenter in pod %s/%s container %s netns %s failed: %w. Output: %s",
			p.namespace, p.name, p.container, p.netnsPath, err, string(out))
	}
	return string(out), nil
}

// add ephemeral container to deal with distroless image
type podDebugExecutor struct {
	namespace string
	name      string
	container string
	image     string
}

func ForPodDebug(namespace, name, container, image string) Executor {
	return &podDebugExecutor{
		namespace: namespace,
		name:      name,
		container: container,
		image:     image,
	}
}

func (pd *podDebugExecutor) Exec(cmd string, args ...string) (string, error) {
	if Kubectl == "" {
		return "", errors.New("the kubectl parameter is not set")
	}

	imageArg := "--image=" + pd.image
	targetArg := "--target=" + pd.container
	debuggerArg := pd.container + "-debugger-" + utilrand.String(5)

	fullargs := append([]string{"debug", "-it", "-n", pd.namespace, "-c",
		debuggerArg, targetArg, imageArg, pd.name, "--", cmd}, args...)

	out, err := exec.Command(Kubectl, fullargs...).CombinedOutput()
	return string(out), err
}
