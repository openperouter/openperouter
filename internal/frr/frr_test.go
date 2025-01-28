// SPDX-License-Identifier:Apache-2.0

package frr

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openperouter/openperouter/internal/ipfamily"
	"k8s.io/apimachinery/pkg/util/wait"
)

const testData = "testdata/"

var update = flag.Bool("update", false, "update .golden files")

func TestBasic(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{
		Underlay: UnderlayConfig{
			MyASN: 64512,
			VTEP:  "100.64.0.1/32",
			Neighbors: []NeighborConfig{
				{
					ASN:      64512,
					Addr:     "192.168.1.2",
					IPFamily: ipfamily.IPv4,
				},
			},
		},
		VNIs: []VNIConfig{
			{
				VRF: "red",
				ASN: 64512,
				VNI: 100,
				LocalNeighbor: &NeighborConfig{
					ASN:      64512,
					Addr:     "192.168.1.2",
					IPFamily: ipfamily.IPv4,
				},
				ToAdvertise: []string{
					"192.169.10.2/24",
				},
			},
		},
	}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func TestEmpty(t *testing.T) {
	configFile := testSetup(t)
	updater := testUpdater(configFile)

	config := Config{}
	if err := ApplyConfig(context.TODO(), &config, updater); err != nil {
		t.Fatalf("Failed to apply config: %s", err)
	}

	testCheckConfigFile(t)
}

func testCompareFiles(t *testing.T, configFile, goldenFile string) {
	var lastError error

	// Try comparing files multiple times because tests can generate more than one configuration
	err := wait.PollUntilContextTimeout(context.TODO(), 10*time.Millisecond, 2*time.Second, true, func(ctx context.Context) (bool, error) {
		lastError = nil
		cmd := exec.Command("diff", configFile, goldenFile)
		output, err := cmd.Output()

		if err != nil {
			lastError = fmt.Errorf("command %s returned error: %s\n%s", cmd.String(), err, output)
			return false, nil
		}

		return true, nil
	})

	// err can only be a ErrWaitTimeout, as the check function always return nil errors.
	// So lastError is always set
	if err != nil {
		t.Fatalf("failed to compare configfiles %s, %s using poll interval\nlast error: %v", configFile, goldenFile, lastError)
	}
}

func testUpdateGoldenFile(t *testing.T, configFile, goldenFile string) {
	t.Log("update golden file")

	// Sleep to be sure the sessionManager has produced all configuration the test
	// has triggered and no config is still waiting in the debouncer() local variables.
	// No other conditions can be checked, so sleeping is our best option.
	time.Sleep(100 * time.Millisecond)

	cmd := exec.Command("cp", "-a", configFile, goldenFile)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("command %s returned %s and error: %s", cmd.String(), output, err)
	}
}

func testGenerateFileNames(t *testing.T) (string, string) {
	return filepath.Join(testData, filepath.FromSlash(t.Name())), filepath.Join(testData, filepath.FromSlash(t.Name())+".golden")
}

func testSetup(t *testing.T) string {
	configFile, _ := testGenerateFileNames(t)
	_ = os.Remove(configFile) // removing leftovers from previous runs
	return configFile
}

func testCheckConfigFile(t *testing.T) {
	configFile, goldenFile := testGenerateFileNames(t)

	if *update {
		testUpdateGoldenFile(t, configFile, goldenFile)
	}

	testCompareFiles(t, configFile, goldenFile)

	if !strings.Contains(configFile, "Invalid") {
		err := testFileIsValid(configFile)
		if err != nil {
			t.Fatalf("Failed to verify the file %s", err)
		}
	}
}

func testUpdater(configFile string) func(context.Context, string) error {
	return func(_ context.Context, config string) error {
		err := os.WriteFile(configFile, []byte(config), 0600)
		if err != nil {
			return fmt.Errorf("failed to write the config to %s", configFile)
		}
		return nil
	}
}
