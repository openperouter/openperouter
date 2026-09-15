// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"errors"

	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	frrContainer testcontainers.Container
	frrDir       string
)

const (
	openperouterImage = "quay.io/openperouter/router:main"
)

func init() {
	osHostname = func() (string, error) {
		return "hostname", nil
	}
}

func TestMain(m *testing.M) {
	// override reloadConfig so it doesn't try to reload it.

	flag.Parse()
	if !testing.Short() {
		os.Exit(testWithDocker(m))
	}
	os.Exit(m.Run())
}

func testWithDocker(m *testing.M) (code int) {
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
	daemonsFile := filepath.Join(cwd, "testdata/daemons")
	vtyshFile := filepath.Join(cwd, "testdata/vtysh.conf")

	req := testcontainers.ContainerRequest{
		Image:      openperouterImage,
		Entrypoint: []string{"/sbin/tini", "--"},
		Cmd:        []string{"/usr/lib/frr/docker-start"},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Binds = append(hc.Binds, fmt.Sprintf("%s:/etc/tempfrr", frrDir))
			hc.Binds = append(hc.Binds, fmt.Sprintf("%s:/etc/frr/daemons", daemonsFile))
			hc.Binds = append(hc.Binds, fmt.Sprintf("%s:/etc/frr/vtysh.conf", vtyshFile))
			hc.CapAdd = append(hc.CapAdd, "cap_net_bind_service", "cap_net_raw", "cap_sys_admin")
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

	return m.Run()
}

type invalidFileErr struct {
	Reason string
}

func (e invalidFileErr) Error() string {
	return e.Reason
}

func frrReload(fileName, mode string) error {
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
		return invalidFileErr{Reason: fmt.Sprintf("code: %d, buffer out: %q", code, bufOut.String())}
	}
	return nil
}
