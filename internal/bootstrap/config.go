// SPDX-License-Identifier:Apache-2.0

// Package bootstrap renders the systemd Quadlets and host configuration
// files of a hostmode OpenPERouter deployment from a single YAML file.
package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	yamlv2 "go.yaml.in/yaml/v2"
)

const (
	datapathKernel = "kernel"

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
	Image                                string                `json:"image,omitempty" yaml:"image,omitempty"`
	Datapath                             string                `json:"datapath,omitempty" yaml:"datapath,omitempty"`
	LogLevel                             string                `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
	OVSSocketPath                        string                `json:"ovsSocketPath,omitempty" yaml:"ovsSocketPath,omitempty"`
	OVSRunDir                            string                `json:"ovsRunDir,omitempty" yaml:"ovsRunDir,omitempty"`
	HealthProbePort                      int                   `json:"healthProbePort,omitempty" yaml:"healthProbePort,omitempty"`
	BGPListenLimit                       int                   `json:"bgpListenLimit,omitempty" yaml:"bgpListenLimit,omitempty"`
	ControllerMaxReconcileBackoffSeconds *int                  `json:"controllerMaxReconcileBackoffSeconds,omitempty" yaml:"controllerMaxReconcileBackoffSeconds,omitempty"`
	RuntimeConfiguration                 *RuntimeConfiguration `json:"runtimeConfiguration" yaml:"runtimeConfiguration"`
}

// RuntimeConfiguration locates the separate runtime configuration files.
type RuntimeConfiguration struct {
	Directory string `json:"directory" yaml:"directory"`
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
	decoder := yamlv2.NewDecoder(bytes.NewReader(data))
	decoder.SetStrict(true)
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
	if deployment.RuntimeConfiguration == nil {
		return errors.New("validate: runtimeConfiguration: required")
	}
	if deployment.RuntimeConfiguration.Directory == "" {
		return errors.New("validate: runtimeConfiguration.directory: required")
	}
	if err := validateHostPath(deployment.RuntimeConfiguration.Directory); err != nil {
		return fmt.Errorf("validate: runtimeConfiguration.directory: %w", err)
	}
	if deployment.Image == "" {
		return errors.New("validate: image: required")
	}
	if strings.ContainsAny(deployment.Image, " \t\n\"'\\%$") {
		return fmt.Errorf("validate: image: invalid image reference %q", deployment.Image)
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
	if deployment.HealthProbePort < 1 || deployment.HealthProbePort > maxPort {
		return fmt.Errorf("validate: healthProbePort: %d out of range 1-%d", deployment.HealthProbePort, maxPort)
	}
	if deployment.HealthProbePort == routerHealthPort {
		return fmt.Errorf("validate: healthProbePort: %d is used by the router health check",
			routerHealthPort)
	}
	if deployment.BGPListenLimit < 1 || deployment.BGPListenLimit > maxPort {
		return fmt.Errorf("validate: bgpListenLimit: %d out of range 1-%d", deployment.BGPListenLimit, maxPort)
	}
	if b := deployment.ControllerMaxReconcileBackoffSeconds; b != nil {
		if *b < 1 {
			return fmt.Errorf("validate: controllerMaxReconcileBackoffSeconds: must be at least 1, got %d", *b)
		}
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
