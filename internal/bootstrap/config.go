// SPDX-License-Identifier:Apache-2.0

// Package bootstrap renders the systemd Quadlets and host configuration
// files of a hostmode OpenPERouter deployment from a single YAML file.
package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	datapathKernel             = "kernel"
	maxReconcileBackoffSeconds = math.MaxInt64 / int64(time.Second)

	defaultLogLevel        = "info"
	defaultOVSRunDir       = "/var/run/openvswitch"
	defaultOVSSocketPath   = "unix:/var/run/openvswitch/db.sock"
	defaultHealthProbePort = 9081
	defaultBGPListenLimit  = 65535
	routerHealthPort       = 9080
	maxPort                = 65535
)

var supportedLogLevels = []string{"debug", "info", "warn", "error"}

// Deployment holds the operator-compatible deployment knobs.
type Deployment struct {
	// Image is the router image used by the generated Quadlets.
	Image string `yaml:"image"`
	// Datapath selects the forwarding implementation.
	Datapath string `yaml:"datapath"`
	// LogLevel sets the controller log level.
	LogLevel string `yaml:"logLevel"`
	// OVSSocketPath is the Unix socket address used to connect to OVS.
	OVSSocketPath string `yaml:"ovsSocketPath"`
	// OVSRunDir is the host directory containing the OVS socket.
	OVSRunDir string `yaml:"ovsRunDir"`
	// HealthProbePort is the controller health endpoint port.
	HealthProbePort int `yaml:"healthProbePort"`
	// BGPListenLimit sets the FRR BGP listen limit.
	BGPListenLimit int `yaml:"bgpListenLimit"`
	// ControllerMaxReconcileBackoffSeconds caps the delay between reconcile retries.
	ControllerMaxReconcileBackoffSeconds *int64 `yaml:"controllerMaxReconcileBackoffSeconds"`
}

// ParseConfig strictly decodes, defaults, and validates a boot configuration.
func ParseConfig(data []byte) (Deployment, error) {
	deployment := Deployment{
		Datapath:        datapathKernel,
		LogLevel:        defaultLogLevel,
		OVSSocketPath:   defaultOVSSocketPath,
		OVSRunDir:       defaultOVSRunDir,
		HealthProbePort: defaultHealthProbePort,
		BGPListenLimit:  defaultBGPListenLimit,
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	// Reject unknown keys so typos cannot silently leave defaults in effect.
	decoder.KnownFields(true)
	if err := decoder.Decode(&deployment); err != nil {
		return Deployment{}, fmt.Errorf("parse: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Deployment{}, errors.New("parse: expected exactly one YAML document")
	}
	if err := validateConfig(deployment); err != nil {
		return Deployment{}, err
	}
	return deployment, nil
}

func validateConfig(deployment Deployment) error {
	if err := validateImage(deployment.Image); err != nil {
		return err
	}

	if dp := deployment.Datapath; dp != datapathKernel {
		return fmt.Errorf("validate: datapath: %q is not supported, only %q", dp, datapathKernel)
	}
	if !slices.Contains(supportedLogLevels, deployment.LogLevel) {
		return fmt.Errorf("validate: logLevel: %q is not supported, expected one of %v",
			deployment.LogLevel, supportedLogLevels)
	}
	if err := validateHostPath(deployment.OVSRunDir); err != nil {
		return fmt.Errorf("validate: ovsRunDir: %w", err)
	}
	if err := validateOVSSocket(deployment.OVSSocketPath, deployment.OVSRunDir); err != nil {
		return fmt.Errorf("validate: ovsSocketPath: %w", err)
	}
	if err := validateHealthProbePort(deployment.HealthProbePort); err != nil {
		return err
	}
	if deployment.BGPListenLimit < 1 || deployment.BGPListenLimit > maxPort {
		return fmt.Errorf("validate: bgpListenLimit: %d out of range 1-%d", deployment.BGPListenLimit, maxPort)
	}
	if b := deployment.ControllerMaxReconcileBackoffSeconds; b != nil {
		if *b < 1 || *b > maxReconcileBackoffSeconds {
			return fmt.Errorf("validate: controllerMaxReconcileBackoffSeconds: must be between 1 and %d, got %d", maxReconcileBackoffSeconds, *b)
		}
	}
	return nil
}

func validateImage(image string) error {
	if image == "" {
		return errors.New("validate: image: required")
	}
	if strings.ContainsAny(image, " \t\n\"'\\%$") {
		return fmt.Errorf("validate: image: invalid image reference %q", image)
	}
	return nil
}

func validateHealthProbePort(port int) error {
	if port < 1 || port > maxPort {
		return fmt.Errorf("validate: healthProbePort: %d out of range 1-%d", port, maxPort)
	}
	if port == routerHealthPort {
		return fmt.Errorf("validate: healthProbePort: %d is used by the router health check",
			routerHealthPort)
	}
	return nil
}

func validateHostPath(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return fmt.Errorf("%q must be a clean absolute path", p)
	}
	if strings.ContainsAny(p, " \t\n:%$\"'\\") {
		return fmt.Errorf("%q contains unsupported characters", p)
	}
	return nil
}

func validateOVSSocket(socket, runDir string) error {
	path, ok := strings.CutPrefix(socket, "unix:")
	if !ok {
		return fmt.Errorf("%q must be a unix: socket address", socket)
	}
	if err := validateHostPath(path); err != nil {
		return err
	}
	if filepath.Dir(path) != runDir {
		return fmt.Errorf("%q must be located in ovsRunDir %q", socket, runDir)
	}
	return nil
}
