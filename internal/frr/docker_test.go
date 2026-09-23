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
	frrImageTag = "10.6.0"
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
		testWithDocker(m)
		return
	}
	m.Run()
}

func testWithDocker(m *testing.M) {
	ctx := context.Background()

	var err error
	frrDir, err = os.MkdirTemp("/tmp", "frr_integration")
	if err != nil {
		log.Fatalf("failed to create temp dir %s", err)
	}

	req := testcontainers.ContainerRequest{
		Image: fmt.Sprintf("quay.io/frrouting/frr:%s", frrImageTag),
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Binds = append(hc.Binds, fmt.Sprintf("%s:/etc/tempfrr", frrDir))
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

	cmd := exec.Command("cp", "testdata/vtysh.conf", filepath.Join(frrDir, "vtysh.conf"))
	res, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("failed to move vtysh.conf to %s - %s - %s", frrDir, err, res)
	}

	code, _, err := frrContainer.Exec(ctx, []string{"cp", "/etc/tempfrr/vtysh.conf", "/etc/frr/vtysh.conf"})
	if err != nil || code != 0 {
		log.Fatalf("failed to move vtysh.conf inside the container - res %d %s", code, err)
	}

	retCode := m.Run()

	if err := frrContainer.Terminate(ctx); err != nil {
		log.Fatalf("failed to terminate container %s", err)
	}
	if err := os.RemoveAll(frrDir); err != nil {
		log.Fatalf("failed to remove all from %s - %s", frrDir, err)
	}

	os.Exit(retCode)
}

type invalidFileErr struct {
	Reason string
}

func (e invalidFileErr) Error() string {
	return e.Reason
}

func testFileIsValid(fileName string) error {
	if testing.Short() {
		return nil
	}
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

	bufErr := new(bytes.Buffer)
	bufOut := new(bytes.Buffer)
	code, reader, err := frrContainer.Exec(ctx, []string{"python3", "/usr/lib/frr/frr-reload.py", "--test", "--stdout", "/etc/frr/frr.conf"})
	if err != nil {
		return errors.Join(err, errors.New("failed to exec reloader into the container"))
	}

	if reader != nil {
		_, _ = bufOut.ReadFrom(reader)
	}

	if code != 0 {
		return invalidFileErr{Reason: fmt.Sprintf("code: %d, buffer out: %q, buffer err: %q", code, bufOut.String(), bufErr.String())}
	}
	return nil
}

func TestDockerFRRFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FRR integration")
	}

	badFile := filepath.Join(testData, "TestDockerTestfails.golden")
	err := testFileIsValid(badFile)
	if !errors.As(err, &invalidFileErr{}) {
		t.Fatalf("Validity check of invalid file passed")
	}
}
