// SPDX-License-Identifier:Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

var errAlreadyRunning = errors.New("already running")

type pidFile string

// Unlock simply deletes the pid file.
func (p pidFile) Unlock() error {
	return os.Remove(string(p))
}

// Lock writes our PID to the pid file. If a previous process is still
// running, it returns errAlreadyRunning.
func (p pidFile) Lock() error {
	currentPid := os.Getpid()

	b, err := os.ReadFile(string(p))
	if err != nil {
		return os.WriteFile(string(p), []byte(strconv.Itoa(currentPid)), 0644)
	}

	pid, err := strconv.Atoi(strings.Trim(string(b), "\n"))
	if err != nil {
		return err
	}
	proc, _ := os.FindProcess(pid)
	if err := proc.Signal(syscall.Signal(0)); !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("process with PID %d: %w", pid, errAlreadyRunning)
	}
	return os.WriteFile(string(p), []byte(strconv.Itoa(currentPid)), 0644)
}
