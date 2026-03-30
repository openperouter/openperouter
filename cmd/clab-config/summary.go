// SPDX-License-Identifier:Apache-2.0

package main

import (
	"encoding/json"
	"fmt"

	clabconfig "github.com/openperouter/openperouter/internal/clabconfig"
	"github.com/spf13/cobra"
)

func summaryCmd() *cobra.Command {
	var statePath, outputFormat string

	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Display configuration summary from a state file",
		Long: `Summary loads a previously generated topology-state.json file and displays
the full allocation summary including nodes, links, IP assignments, and
VTEP addresses. Output can be formatted as human-readable text or JSON.`,
		Example: `  # Display a human-readable summary
  clab-config summary --state /tmp/singlecluster-output/topology-state.json

  # Output the full state as JSON
  clab-config summary --state topology-state.json -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := clabconfig.LoadState(statePath)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			switch outputFormat {
			case "text":
				fmt.Println(state.Summary())
			case "json":
				data, err := json.MarshalIndent(state, "", "  ")
				if err != nil {
					return fmt.Errorf("marshalling state to JSON: %w", err)
				}
				fmt.Println(string(data))
			default:
				return fmt.Errorf("unsupported output format: %s", outputFormat)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&statePath, "state", "", "Path to topology-state.json file")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format (text or json)")
	_ = cmd.MarkFlagRequired("state")

	return cmd
}
