// SPDX-License-Identifier:Apache-2.0

package bootstrap

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testImage = "quay.io/openperouter/router:main"

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    Deployment
	}{
		{
			name: "defaults", fixture: "minimal",
			want: Deployment{
				Image: testImage, Datapath: "kernel", LogLevel: "info", OVSRunDir: "/var/run/openvswitch",
				OVSSocketPath: "unix:/var/run/openvswitch/db.sock", HealthProbePort: 9081, BGPListenLimit: 65535,
				RuntimeConfiguration: &RuntimeConfiguration{Directory: "/etc/openperouter"},
			},
		},
		{
			name: "customized", fixture: "customized",
			want: Deployment{
				Image: "quay.io/openperouter/router:v0.1.0", Datapath: "kernel", LogLevel: "debug",
				OVSRunDir: "/run/openvswitch", OVSSocketPath: "unix:/run/openvswitch/db.sock",
				HealthProbePort: 9181, BGPListenLimit: 4096, ControllerMaxReconcileBackoffSeconds: new(30),
				RuntimeConfiguration: &RuntimeConfiguration{Directory: "/etc/openperouter"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tt.fixture + ".yaml")
			require.NoError(t, err)
			got, err := ParseConfig(data)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseConfigOVSSocket(t *testing.T) {
	tests := []struct{ name, fields, want string }{
		{"default directory", "", "unix:/var/run/openvswitch/db.sock"},
		{"custom directory", "ovsRunDir: /run/ovs\novsSocketPath: unix:/run/ovs/db.sock", "unix:/run/ovs/db.sock"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConfig([]byte(bootConfig(tt.fields)))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.OVSSocketPath)
		})
	}
}

func TestParseConfigLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(bootConfig("logLevel: " + level)))
			require.NoError(t, err)
			assert.Equal(t, level, cfg.LogLevel)
		})
	}
}

func TestParseConfigErrors(t *testing.T) {
	valid := bootConfig("")
	tests := []struct{ name, input, wantErr string }{
		{"version removed", valid + "version: 1\n", "field version not found"},
		{"unknown root key", valid + "foo: bar\n", "field foo not found"},
		{"duplicate key", valid + "image: duplicate\n", "already set"},
		{"unknown deployment key", bootConfig("foo: 1"), "field foo not found"},
		{"kubernetes object", valid + "apiVersion: v1\n", "field apiVersion not found"},
		{"nodeSelector", bootConfig("nodeSelector: {}"), "field nodeSelector not found"},
		{"tolerations", bootConfig("tolerations: []"), "field tolerations not found"},
		{"affinity", bootConfig("affinity: {}"), "field affinity not found"},
		{"priorityClassName", bootConfig("priorityClassName: x"), "field priorityClassName not found"},
		{"resources", bootConfig("resources: {}"), "field resources not found"},
		{"grout section", bootConfig("grout: {}"), "field grout not found"},
		{"grout datapath", bootConfig("datapath: grout"), "datapath"},
		{"loglevel all", bootConfig("logLevel: all"), "logLevel"},
		{"loglevel none", bootConfig("logLevel: none"), "logLevel"},
		{"no image", strings.Replace(valid, "image: "+testImage, "image: ''", 1), "image: required"},
		{"no image field", "runtimeConfiguration: {directory: /etc/r}\n", "image: required"},
		{"bad image", strings.Replace(valid, testImage, "a b", 1), "image"},
		{"health port zero", bootConfig("healthProbePort: 0"), "healthProbePort"},
		{"health port too high", bootConfig("healthProbePort: 70000"), "healthProbePort"},
		{"health port router", bootConfig("healthProbePort: 9080"), "router health check"},
		{"listen limit zero", bootConfig("bgpListenLimit: 0"), "bgpListenLimit"},
		{"listen limit high", bootConfig("bgpListenLimit: 65536"), "bgpListenLimit"},
		{"backoff zero", bootConfig("controllerMaxReconcileBackoffSeconds: 0"), "controllerMaxReconcileBackoffSeconds"},
		{"ovs run dir relative", bootConfig("ovsRunDir: run/ovs"), "ovsRunDir"},
		{"custom run directory needs full socket", bootConfig("ovsRunDir: /run/ovs"), "ovsSocketPath"},
		{"ovs socket not unix", bootConfig("ovsSocketPath: tcp:1.2.3.4:6640"), "ovsSocketPath"},
		{"ovs socket outside run dir", bootConfig("ovsSocketPath: unix:/tmp/db.sock"), "ovsRunDir"},
		{"frr daemons file", bootConfig("frrDaemonsFile: /etc/daemons"), "field frrDaemonsFile not found"},
		{"node belongs in node file", valid + "node: {}\n", "field node not found"},
		{"missing runtime configuration", strings.Split(valid, "runtimeConfiguration:")[0], "runtimeConfiguration: required"},
		{"inline rejected", strings.Replace(valid, "directory: /etc/r", "directory: /etc/r, inline: {}", 1), "field inline not found"},
		{"missing directory", strings.Replace(valid, "directory: /etc/r", "", 1), "runtimeConfiguration.directory: required"},
		{"relative directory", strings.Replace(valid, "directory: /etc/r", "directory: etc/r", 1), "runtimeConfiguration.directory"},
		{"unclean directory", strings.Replace(valid, "directory: /etc/r", "directory: /etc/../r", 1), "runtimeConfiguration.directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.input))
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestShippedExampleIsValid(t *testing.T) {
	data, err := os.ReadFile("../../systemdmode/bootstrap/openpe_boot.yaml.example")
	require.NoError(t, err)
	_, err = ParseConfig(data)
	require.NoError(t, err)
}

func bootConfig(fields string) string {
	return "image: " + testImage + "\n" + fields + "\nruntimeConfiguration: {directory: /etc/r}\n"
}
