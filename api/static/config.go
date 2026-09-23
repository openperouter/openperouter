// SPDX-License-Identifier:Apache-2.0

package static

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
)

type NodeIndex struct {
	Index         int    `json:"index"`
	InterfaceName string `json:"interfaceName"`
	CIDR          string `json:"cidr,omitempty"`
}

type NodeConfig struct {
	NodeIndex NodeIndex `json:"nodeIndex"`
	NodeName  string    `json:"nodeName"`
	LogLevel  string    `json:"logLevel"`
}

// StaticL3VNI wraps an L3VNISpec with a required name field for static
// configuration. The name becomes the L3VNI's metadata.name.
type StaticL3VNI struct {
	// name becomes the metadata.name of the L3VNI.
	Name               string `json:"name" yaml:"name"`
	v1alpha1.L3VNISpec `json:",inline" yaml:",inline"`
}

// StaticL2VNI wraps an L2VNISpec with a required name field for static
// configuration. The name becomes the L2VNI's metadata.name.
type StaticL2VNI struct {
	// name becomes the metadata.name of the L2VNI.
	Name               string `json:"name" yaml:"name"`
	v1alpha1.L2VNISpec `json:",inline" yaml:",inline"`
}

// StaticL3VPN wraps an L3VPNSpec with a required name field for static
// configuration. The name becomes the L3VPN's metadata.name.
type StaticL3VPN struct {
	// name becomes the metadata.name of the L3VPN.
	Name               string `json:"name" yaml:"name"`
	v1alpha1.L3VPNSpec `json:",inline" yaml:",inline"`
}

// StaticNeighbor wraps a Neighbor with a password field for static
// configuration. The CRD API does not expose a plaintext password —
// Kubernetes users must use passwordSecret instead. In systemd mode
// there are no Secrets, so the password is set directly here.
type StaticNeighbor struct {
	v1alpha1.Neighbor `json:",inline" yaml:",inline"`
	Password          *string `json:"password,omitempty" yaml:"password,omitempty"`
}

// StaticUnderlaySpec mirrors UnderlaySpec but uses StaticNeighbor so
// that plaintext passwords can be deserialized from static config files.
type StaticUnderlaySpec struct {
	v1alpha1.UnderlaySpec `json:",inline" yaml:",inline"`
	// Neighbors intentionally shadows UnderlaySpec.Neighbors (same JSON
	// key) so that YAML deserialization populates StaticNeighbor instead
	// of Neighbor. Access the embedded UnderlaySpec.Neighbors is always
	// nil after deserialization — use this field instead.
	Neighbors []StaticNeighbor `json:"neighbors,omitempty" yaml:"neighbors,omitempty"`
}

type PERouterConfig struct {
	Underlays      []StaticUnderlaySpec        `yaml:"underlays"`
	L2VNIs         []StaticL2VNI               `yaml:"l2vnis"`
	L3VNIs         []StaticL3VNI               `yaml:"l3vnis"`
	L3VPNs         []StaticL3VPN               `yaml:"l3vpns"`
	BGPPassthrough v1alpha1.L3PassthroughSpec  `yaml:"bgppassthrough"`
	RawFRRConfigs  []v1alpha1.RawFRRConfigSpec `yaml:"rawfrrconfigs"`
}
