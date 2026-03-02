// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"strings"

	clabconfig "github.com/openperouter/openperouter/internal/clabconfig"
	"github.com/spf13/cobra"
)

func queryCmd() *cobra.Command {
	var statePath string

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query topology state for specific information",
		Long: `Query provides subcommands to extract specific pieces of information from
a topology-state.json file. This is useful for scripting and automation
where you need individual values such as VTEP IPs, link addresses, or
node lists.`,
	}

	cmd.PersistentFlags().StringVar(&statePath, "state", "", "Path to topology-state.json file")
	_ = cmd.MarkPersistentFlagRequired("state")

	cmd.AddCommand(nodeVTEPCmd(&statePath))
	cmd.AddCommand(linkIPCmd(&statePath))
	cmd.AddCommand(ipOwnerCmd(&statePath))
	cmd.AddCommand(nodesCmd(&statePath))

	return cmd
}

func nodeVTEPCmd(statePath *string) *cobra.Command {
	var node string

	cmd := &cobra.Command{
		Use:   "node-vtep",
		Short: "Get the VTEP IP for a node",
		Long: `Node-vtep returns the VXLAN Tunnel Endpoint (VTEP) IP address assigned to
the specified node. The output is a single IP address printed to stdout,
suitable for use in shell scripts and pipeline automation.`,
		Example: `  # Get the VTEP IP for a leaf node
  clab-config query --state topology-state.json node-vtep --node leaf0

  # Use in a script
  VTEP=$(clab-config query --state topology-state.json node-vtep --node leaf0)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := clabconfig.LoadState(*statePath)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			vtep, err := state.GetNodeVTEP(node)
			if err != nil {
				return err
			}

			fmt.Println(vtep)
			return nil
		},
	}

	cmd.Flags().StringVar(&node, "node", "", "Node name")
	_ = cmd.MarkFlagRequired("node")

	return cmd
}

func linkIPCmd(statePath *string) *cobra.Command {
	var node, peer, family string

	cmd := &cobra.Command{
		Use:   "link-ip",
		Short: "Get the IP address for a link between two nodes",
		Long: `Link-ip returns the IP address assigned to a node's side of a point-to-point
link with a peer node. You can request either the IPv4 or IPv6 address using
the --family flag. The output is a single IP address in CIDR notation.`,
		Example: `  # Get the IPv4 address for leaf0's link to spine0
  clab-config query --state topology-state.json link-ip --node leaf0 --peer spine0

  # Get the IPv6 address for the same link
  clab-config query --state topology-state.json link-ip --node leaf0 --peer spine0 --family ipv6`,
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := clabconfig.LoadState(*statePath)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			ipFamily := clabconfig.IPv4
			if strings.EqualFold(family, "ipv6") {
				ipFamily = clabconfig.IPv6
			}

			ip, err := state.GetLinkIP(node, peer, ipFamily)
			if err != nil {
				return err
			}

			fmt.Println(ip)
			return nil
		},
	}

	cmd.Flags().StringVar(&node, "node", "", "Node name")
	cmd.Flags().StringVar(&peer, "peer", "", "Peer node name")
	cmd.Flags().StringVar(&family, "family", "ipv4", "IP address family (ipv4 or ipv6)")
	_ = cmd.MarkFlagRequired("node")
	_ = cmd.MarkFlagRequired("peer")

	return cmd
}

func ipOwnerCmd(statePath *string) *cobra.Command {
	var ip string

	cmd := &cobra.Command{
		Use:   "ip-owner",
		Short: "Find the node that owns a given IP address",
		Long: `Ip-owner performs a reverse lookup to find which node and interface own the
given IP address. The output is the node name and interface name separated
by a space, useful for debugging connectivity issues.`,
		Example: `  # Find which node owns a specific IP
  clab-config query --state topology-state.json ip-owner --ip 10.0.0.1

  # Use in a script to get just the node name
  NODE=$(clab-config query --state topology-state.json ip-owner --ip 10.0.0.1 | awk '{print $1}')`,
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := clabconfig.LoadState(*statePath)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			nodeName, iface, err := state.FindIPOwner(ip)
			if err != nil {
				return err
			}

			fmt.Printf("%s %s\n", nodeName, iface)
			return nil
		},
	}

	cmd.Flags().StringVar(&ip, "ip", "", "IP address to look up")
	_ = cmd.MarkFlagRequired("ip")

	return cmd
}

func nodesCmd(statePath *string) *cobra.Command {
	var pattern string

	cmd := &cobra.Command{
		Use:   "nodes",
		Short: "List nodes matching a regex pattern",
		Long: `Nodes lists all node names from the topology state that match the given
regular expression pattern. Each matching node name is printed on its own
line. This is useful for iterating over groups of nodes in shell scripts.`,
		Example: `  # List all leaf nodes
  clab-config query --state topology-state.json nodes --pattern "leaf.*"

  # List all nodes
  clab-config query --state topology-state.json nodes --pattern ".*"

  # Iterate over spine nodes in a script
  for node in $(clab-config query --state topology-state.json nodes --pattern "spine.*"); do
    echo "Processing $node"
  done`,
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := clabconfig.LoadState(*statePath)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			nodes, err := state.GetNodesByPattern(pattern)
			if err != nil {
				return err
			}

			for _, n := range nodes {
				fmt.Println(n)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&pattern, "pattern", "", "Regex pattern to match node names")
	_ = cmd.MarkFlagRequired("pattern")

	return cmd
}
