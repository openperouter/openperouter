// SPDX-License-Identifier:Apache-2.0

package static

type PERouterConfig struct {
	// Node Index is the index assigned to this node. It is used
	// to generate IPs from the CIDRs provided by the user and meant to be
	// assigned at deployment time with a different value on each node.
	NodeIndex int `yaml:"nodeIndex"`
}
