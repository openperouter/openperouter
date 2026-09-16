// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func startMonitor(logFile, pidFile string, intervalSecs int) error {
	if intervalSecs <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}

	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	cmd := exec.Command(self, "record",
		"--log", logFile,
		"--interval", strconv.Itoa(intervalSecs))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting monitor: %w", err)
	}

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return fmt.Errorf("writing pid file: %w", err)
	}

	fmt.Printf("Resource monitor started (pid=%d, interval=%ds, log=%s)\n",
		cmd.Process.Pid, intervalSecs, logFile)
	return nil
}

func stopMonitor(pidFile string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		proc.Signal(syscall.SIGTERM)
	}
	os.Remove(pidFile)
}
