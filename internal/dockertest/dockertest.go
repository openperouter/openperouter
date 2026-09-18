// SPDX-License-Identifier:Apache-2.0

package dockertest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	openperouterImage = "quay.io/openperouter/router:main"
)

var (
	frrContainer testcontainers.Container
	frrDir       string
)

// TestWithDockerM wraps an entire testing.M into the docker test logic (set up the docker test
// container, run all tests with m.Run(), and tear down the container). Used in frr unit tests.
// This version of the docker container runs unprivileged and does not start any daemons.
func TestWithDockerM(m *testing.M) int {
	var code int
	testWithDocker(
		func() { code = m.Run() },
		func(hc *container.HostConfig) {},
	)
	return code
}

// TestWithDockerT wraps a single run of a test function into the docker test logic (set up the
// docker test container, run the single test, and tear down the container). Used in frrconfig
// unit tests of the Update() logic.
// This version of the docker container runs with higher privileges and starts required FRR daemons.
func TestWithDockerT(t *testing.T, testRunner func(t *testing.T)) {
	testWithDocker(
		func() { testRunner(t) },
		func(hc *container.HostConfig) {
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatalf("failed to get current working dir, err: %q", err)
			}
			daemonsFile := filepath.Join(cwd, "../dockertest/testdata/daemons")
			hc.Binds = append(hc.Binds, fmt.Sprintf("%s:/etc/frr/daemons", daemonsFile))
			hc.CapAdd = append(hc.CapAdd, "cap_net_bind_service", "cap_net_raw", "cap_sys_admin")
		},
	)
}

func testWithDocker(testRunner func(), additionalHostConfig func(hc *container.HostConfig)) {
	ctx := context.Background()

	var err error
	frrDir, err = os.MkdirTemp("/tmp", "frr_integration")
	if err != nil {
		log.Fatalf("failed to create temp dir %s", err)
	}
	defer func() {
		if err := os.RemoveAll(frrDir); err != nil {
			log.Fatalf("failure cleaning up tmp dir %s, err: %q", frrDir, err)
		}
	}()

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to get current working dir")
	}
	vtyshFile := filepath.Join(cwd, "../dockertest/testdata/vtysh.conf")

	req := testcontainers.ContainerRequest{
		Image:      openperouterImage,
		Entrypoint: []string{"/sbin/tini", "--"},
		Cmd:        []string{"/usr/lib/frr/docker-start"},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Binds = append(hc.Binds, fmt.Sprintf("%s:/etc/tempfrr", frrDir))
			hc.Binds = append(hc.Binds, fmt.Sprintf("%s:/etc/frr/vtysh.conf", vtyshFile))
			additionalHostConfig(hc)
		},
		WaitingFor: wait.ForExec([]string{"vtysh", "-c", "show version"}),
	}

	frrContainer, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		log.Fatalf("failed to start container %s", err)
	}
	defer func() {
		if err := frrContainer.Terminate(ctx); err != nil {
			log.Fatalf("failed to terminate container %s", err)
		}
	}()

	testRunner()
}

type InvalidFileErr struct {
	Reason string
}

func (e InvalidFileErr) Error() string {
	return e.Reason
}

// FrrReload copies the provided file into the container at /etc/frr/frr.conf and triggers
// a reload of `mode` (`test` or `reload`) inside the running test container.
func FrrReload(fileName, mode string) error {
	cmd := exec.Command("cp", fileName, filepath.Join(frrDir, "frr.conf"))
	res, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Join(err, fmt.Errorf("failed to copy %s to %s: %s", fileName, frrDir, string(res)))
	}

	ctx := context.Background()
	code, _, err := frrContainer.Exec(ctx, []string{"cp", "/etc/tempfrr/frr.conf", "/etc/frr/frr.conf"})
	if err != nil {
		return errors.Join(err, errors.New("failed to copy frr.conf inside the container"))
	}
	if code != 0 {
		return fmt.Errorf("failed to copy frr.conf inside the container, exit code: %d", code)
	}

	bufOut := new(bytes.Buffer)
	code, reader, err := frrContainer.Exec(
		ctx,
		[]string{
			"python3", "/usr/lib/frr/frr-reload.py", fmt.Sprintf("--%s", mode), "--stdout", "/etc/frr/frr.conf",
		},
	)
	if err != nil {
		return errors.Join(err, errors.New("failed to exec reloader into the container"))
	}

	if reader != nil {
		_, _ = bufOut.ReadFrom(reader)
	}

	if code != 0 {
		return InvalidFileErr{Reason: fmt.Sprintf("code: %d, buffer out: %q", code, bufOut.String())}
	}
	return nil
}

func RunVtysh(commands ...string) (string, error) {
	args := make([]string, 0, len(commands)*2+1)
	args = append(args, "vtysh")
	for _, c := range commands {
		args = append(args, "-c", c)
	}

	ctx := context.Background()
	code, reader, err := frrContainer.Exec(ctx, args)
	if err != nil {
		return "", errors.Join(err, errors.New("failed to run vtysh command inside container"))
	}

	bufOut := new(bytes.Buffer)
	if reader != nil {
		_, _ = bufOut.ReadFrom(reader)
	}
	if code != 0 {
		return "", fmt.Errorf("code: %d, buffer out: %q", code, bufOut.String())
	}
	return bufOut.String(), nil
}
