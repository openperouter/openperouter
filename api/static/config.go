// SPDX-License-Identifier:Apache-2.0

package static

import (
	"github.com/openperouter/openperouter/api/v1alpha1"
)

type NodeConfig struct {
	NodeIndex int    `json:"nodeIndex"`
	NodeName  string `json:"nodeName"`
	LogLevel  string `json:"logLevel"`
}

type PERouterConfig struct {
	Underlays      []v1alpha1.UnderlaySpec     `yaml:"underlays"`
	L2VNIs         []v1alpha1.L2VNISpec        `yaml:"l2vnis"`
	L3VNIs         []v1alpha1.L3VNISpec        `yaml:"l3vnis"`
	L3VPNs         []v1alpha1.L3VPNSpec        `yaml:"l3vpns"`
	BGPPassthrough v1alpha1.L3PassthroughSpec  `yaml:"bgppassthrough"`
	RawFRRConfigs  []v1alpha1.RawFRRConfigSpec `yaml:"rawfrrconfigs"`
}
