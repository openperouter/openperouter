// SPDX-License-Identifier:Apache-2.0

package bootstrap

import (
	"os"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

const testImage = "quay.io/openperouter/router:main"

func TestParseConfig(t *testing.T) {
	valid := bootConfig("")
	defaults := Deployment{
		Image:           testImage,
		Datapath:        datapathKernel,
		LogLevel:        defaultLogLevel,
		OVSRunDir:       defaultOVSRunDir,
		OVSSocketPath:   defaultOVSSocketPath,
		HealthProbePort: defaultHealthProbePort,
		BGPListenLimit:  defaultBGPListenLimit,
	}
	customOVS := defaults
	customOVS.OVSRunDir = "/run/ovs"
	customOVS.OVSSocketPath = "unix:/run/ovs/db.sock"

	maxBackoff := defaults
	maxBackoff.ControllerMaxReconcileBackoffSeconds = new(int64(9223372036))

	type testCase struct {
		name    string
		input   string
		want    Deployment
		wantErr string
	}
	logLevelCase := func(level string) testCase {
		want := defaults
		want.LogLevel = level
		return testCase{name: "log level " + level, input: bootConfig("logLevel: " + level), want: want}
	}
	tests := []testCase{
		{name: "defaults", input: valid, want: defaults},
		logLevelCase("debug"),
		logLevelCase("info"),
		logLevelCase("warn"),
		logLevelCase("error"),
		{
			name: "customized",
			input: `image: quay.io/openperouter/router:v0.1.0
datapath: kernel
logLevel: debug
healthProbePort: 9181
bgpListenLimit: 4096
controllerMaxReconcileBackoffSeconds: 30
ovsRunDir: /run/openvswitch
ovsSocketPath: unix:/run/openvswitch/db.sock
`,
			want: Deployment{
				Image: "quay.io/openperouter/router:v0.1.0", Datapath: datapathKernel, LogLevel: "debug",
				OVSRunDir: "/run/openvswitch", OVSSocketPath: "unix:/run/openvswitch/db.sock",
				HealthProbePort: 9181, BGPListenLimit: 4096, ControllerMaxReconcileBackoffSeconds: new(int64(30)),
			},
		},
		{
			name:  "custom OVS directory",
			input: bootConfig("ovsRunDir: /run/ovs\novsSocketPath: unix:/run/ovs/db.sock"),
			want:  customOVS,
		},
		{name: "version removed", input: valid + "version: 1\n", wantErr: "field version not found"},
		{name: "unknown root key", input: valid + "foo: bar\n", wantErr: "field foo not found"},
		{name: "duplicate key", input: valid + "image: duplicate\n", wantErr: "already defined"},
		{name: "unknown deployment key", input: bootConfig("foo: 1"), wantErr: "field foo not found"},
		{name: "kubernetes object", input: valid + "apiVersion: v1\n", wantErr: "field apiVersion not found"},
		{name: "nodeSelector", input: bootConfig("nodeSelector: {}"), wantErr: "field nodeSelector not found"},
		{name: "tolerations", input: bootConfig("tolerations: []"), wantErr: "field tolerations not found"},
		{name: "affinity", input: bootConfig("affinity: {}"), wantErr: "field affinity not found"},
		{name: "priorityClassName", input: bootConfig("priorityClassName: x"), wantErr: "field priorityClassName not found"},
		{name: "resources", input: bootConfig("resources: {}"), wantErr: "field resources not found"},
		{name: "grout section", input: bootConfig("grout: {}"), wantErr: "field grout not found"},
		{name: "grout datapath", input: bootConfig("datapath: grout"), wantErr: "datapath"},
		{name: "loglevel all", input: bootConfig("logLevel: all"), wantErr: "logLevel"},
		{name: "loglevel none", input: bootConfig("logLevel: none"), wantErr: "logLevel"},
		{name: "no image", input: strings.Replace(valid, "image: "+testImage, "image: ''", 1), wantErr: "image: required"},
		{name: "no image field", input: "logLevel: info\n", wantErr: "image: required"},
		{name: "bad image", input: strings.Replace(valid, testImage, "a b", 1), wantErr: "image"},
		{name: "health port zero", input: bootConfig("healthProbePort: 0"), wantErr: "healthProbePort"},
		{name: "health port too high", input: bootConfig("healthProbePort: 70000"), wantErr: "healthProbePort"},
		{name: "health port router", input: bootConfig("healthProbePort: 9080"), wantErr: "router health check"},
		{name: "listen limit zero", input: bootConfig("bgpListenLimit: 0"), wantErr: "bgpListenLimit"},
		{name: "listen limit high", input: bootConfig("bgpListenLimit: 65536"), wantErr: "bgpListenLimit"},
		{name: "maximum backoff", input: bootConfig("controllerMaxReconcileBackoffSeconds: 9223372036"), want: maxBackoff},
		{name: "backoff overflow", input: bootConfig("controllerMaxReconcileBackoffSeconds: 9223372037"),
			wantErr: "controllerMaxReconcileBackoffSeconds: must be between 1 and 9223372036"},
		{name: "backoff zero", input: bootConfig("controllerMaxReconcileBackoffSeconds: 0"), wantErr: "controllerMaxReconcileBackoffSeconds"},
		{name: "ovs run dir relative", input: bootConfig("ovsRunDir: run/ovs"), wantErr: "ovsRunDir"},
		{name: "custom run directory needs full socket", input: bootConfig("ovsRunDir: /run/ovs"), wantErr: "ovsSocketPath"},
		{name: "ovs socket not unix", input: bootConfig("ovsSocketPath: tcp:1.2.3.4:6640"), wantErr: "ovsSocketPath"},
		{name: "ovs socket outside run dir", input: bootConfig("ovsSocketPath: unix:/tmp/db.sock"), wantErr: "ovsRunDir"},
		{name: "frr daemons file", input: bootConfig("frrDaemonsFile: /etc/daemons"), wantErr: "field frrDaemonsFile not found"},
		{name: "node belongs in node file", input: valid + "node: {}\n", wantErr: "field node not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got, err := ParseConfig([]byte(tt.input))
			if tt.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tt.wantErr)))
				return
			}
			g.Expect(err).To(Succeed())
			g.Expect(got).To(Equal(tt.want))
		})
	}
}

func TestShippedExampleIsValid(t *testing.T) {
	g := NewWithT(t)
	data, err := os.ReadFile("../../systemdmode/bootstrap/boot.yaml.example")
	g.Expect(err).To(Succeed())
	_, err = ParseConfig(data)
	g.Expect(err).To(Succeed())
}

func bootConfig(fields string) string {
	return "image: " + testImage + "\n" + fields + "\n"
}
