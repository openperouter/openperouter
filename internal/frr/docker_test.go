// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/openperouter/openperouter/internal/dockertest"
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
		os.Exit(dockertest.TestWithDockerM(m))
	}
	os.Exit(m.Run())
}

func TestDockerFRRFails(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FRR integration")
	}

	badFile := filepath.Join(testData, "TestDockerTestfails.golden")
	err := dockertest.FrrReload(badFile, "test")
	if !errors.As(err, &dockertest.InvalidFileErr{}) {
		t.Fatalf("Validity check of invalid file passed")
	}
}
