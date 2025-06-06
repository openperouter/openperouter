// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"errors"

	"github.com/ory/dockertest/v3"
)

var (
	containerHandle *dockertest.Resource
	frrDir          string
)

const (
	frrImageTag = "10.0.1"
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
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("failed to create dockertest pool %s", err)
	}

	frrDir, err = os.MkdirTemp("/tmp", "frr_integration")
	if err != nil {
		log.Fatalf("failed to create temp dir %s", err)
	}

	containerHandle, err = pool.RunWithOptions(
		&dockertest.RunOptions{
			Name:       "frrtest",
			Repository: "quay.io/frrouting/frr",
			Tag:        frrImageTag,
			Mounts:     []string{fmt.Sprintf("%s:/etc/tempfrr", frrDir)},
		},
	)
	if err != nil {
		log.Fatalf("failed to run container %s", err)
	}

	cmd := exec.Command("cp", "testdata/vtysh.conf", filepath.Join(frrDir, "vtysh.conf"))
	res, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("failed to move vtysh.conf to %s - %s - %s", frrDir, err, res)
	}
	buf := new(bytes.Buffer)
	resCode, err := containerHandle.Exec([]string{"cp", "/etc/tempfrr/vtysh.conf", "/etc/frr/vtysh.conf"},
		dockertest.ExecOptions{
			StdErr: buf,
		})
	if err != nil || resCode != 0 {
		log.Fatalf("failed to move vtysh.conf inside the container - res %d %s %s", resCode, err, buf.String())
	}

	retCode := m.Run()
	// You can't defer this because os.Exit doesn't care for defer
	if err := pool.Purge(containerHandle); err != nil {
		log.Fatalf("failed to purge %s - %s", containerHandle.Container.Name, err)
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
	_, err = containerHandle.Exec([]string{"cp", "/etc/tempfrr/frr.conf", "/etc/frr/frr.conf"},
		dockertest.ExecOptions{})
	if err != nil {
		return errors.Join(err, errors.New("failed to copy frr.conf inside the container"))
	}
	buf := new(bytes.Buffer)
	code, err := containerHandle.Exec([]string{"python3", "/usr/lib/frr/frr-reload.py", "--test", "--stdout", "/etc/frr/frr.conf"},
		dockertest.ExecOptions{
			StdErr: buf,
		})
	if err != nil {
		return errors.Join(err, errors.New("failed to exec reloader into the container"))
	}

	if code != 0 {
		return invalidFileErr{Reason: buf.String()}
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
