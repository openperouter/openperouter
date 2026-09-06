// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	defaultLogFile  = "/tmp/resource_monitor.log"
	defaultPidFile  = "/tmp/resource_monitor.pid"
	defaultInterval = 10
)

type Sample struct {
	Timestamp   string  `json:"ts"`
	LoadAvg1    float64 `json:"load1"`
	MemTotalMi  int     `json:"mem_total_mi"`
	MemAvailMi  int     `json:"mem_avail_mi"`
	DiskAvailMi int     `json:"disk_avail_mi"`
	NrThrottled int64   `json:"nr_throttled"`
	CPUStealPct int     `json:"cpu_steal_pct"`
}

func main() {
	root := &cobra.Command{
		Use:   "ci-monitor-resources",
		Short: "Monitor and analyze CI runner resource usage",
	}

	var logFile, pidFile string
	var interval int

	root.PersistentFlags().StringVar(&logFile, "log", defaultLogFile, "path to the resource monitor log file")
	root.PersistentFlags().StringVar(&pidFile, "pid-file", defaultPidFile, "path to the PID file for the background monitor")

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the background resource monitor",
		RunE: func(_ *cobra.Command, _ []string) error {
			return startMonitor(logFile, pidFile, interval)
		},
	}
	startCmd.Flags().IntVar(&interval, "interval", defaultInterval, "sampling interval in seconds")

	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the background resource monitor",
		RunE: func(_ *cobra.Command, _ []string) error {
			stopMonitor(pidFile)
			return nil
		},
	}

	analyzeCmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze collected resource data and report issues",
		RunE: func(_ *cobra.Command, _ []string) error {
			return analyze(logFile)
		},
	}

	recordCmd := &cobra.Command{
		Use:    "record",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return record(logFile, interval)
		},
	}
	recordCmd.Flags().IntVar(&interval, "interval", defaultInterval, "sampling interval in seconds")

	root.AddCommand(startCmd, stopCmd, analyzeCmd, recordCmd)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
