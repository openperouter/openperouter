// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "clab-config",
		Short: "Containerlab topology configuration tool",
	}

	rootCmd.AddCommand(applyCmd())
	rootCmd.AddCommand(summaryCmd())
	rootCmd.AddCommand(queryCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
