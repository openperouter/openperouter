// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/openperouter/openperouter/internal/clabconfig"
	"github.com/spf13/cobra"
)

func applyCmd() *cobra.Command {
	var clabPath, configPath, outputDir string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Generate topology configuration from clab and environment files",
		Long: `Apply reads a containerlab topology file and an environment configuration,
allocates network resources deterministically, and generates FRR configurations
and setup scripts for each node.

The generated output includes per-node directories containing frr.conf and
optional setup.sh files, plus a topology-state.json that can be used with
the summary and query commands.`,
		Example: `  # Generate configs for singlecluster topology
  clab-config apply --clab clab/singlecluster/kind.clab.yml \
    --config clab/singlecluster/environment-config.yaml \
    --output-dir /tmp/singlecluster-output

  # Generate configs in the current directory
  clab-config apply --clab topology.clab.yml --config env.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			clab, err := clabconfig.LoadClab(clabPath)
			if err != nil {
				return fmt.Errorf("loading clab topology: %w", err)
			}

			config, err := clabconfig.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("loading environment config: %w", err)
			}

			state, warnings, err := clabconfig.Allocate(clab, config)
			if err != nil {
				return fmt.Errorf("allocating resources: %w", err)
			}

			for _, w := range warnings {
				fmt.Fprintf(os.Stderr, "WARNING: %s\n", w)
			}

			files, err := clabconfig.Generate(state)
			if err != nil {
				return fmt.Errorf("generating configuration: %w", err)
			}

			for nodeName, f := range files {
				nodeDir := filepath.Join(outputDir, nodeName)
				if mkErr := os.MkdirAll(nodeDir, 0755); mkErr != nil {
					return fmt.Errorf("creating directory %s: %w", nodeDir, mkErr)
				}

				frrPath := filepath.Join(nodeDir, "frr.conf")
				if writeErr := os.WriteFile(frrPath, []byte(f.FRRConfig), 0644); writeErr != nil {
					return fmt.Errorf("writing %s: %w", frrPath, writeErr)
				}

				if f.SetupScript != "" {
					setupPath := filepath.Join(nodeDir, "setup.sh")
					if writeErr := os.WriteFile(setupPath, []byte(f.SetupScript), 0755); writeErr != nil {
						return fmt.Errorf("writing %s: %w", setupPath, writeErr)
					}
				}
			}

			statePath := filepath.Join(outputDir, "topology-state.json")
			if err := state.SaveState(statePath); err != nil {
				return fmt.Errorf("saving state: %w", err)
			}

			fmt.Println(state.Summary())

			return nil
		},
	}

	cmd.Flags().StringVar(&clabPath, "clab", "", "Path to containerlab topology file")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to environment configuration file")
	cmd.Flags().StringVar(&outputDir, "output-dir", ".", "Directory for generated outputs")
	_ = cmd.MarkFlagRequired("clab")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}
