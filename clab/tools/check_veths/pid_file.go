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

type pidFile string

// Unlock simply deletes the pid file.
func (p pidFile) Unlock() error {
	return os.Remove(string(p))
}

// Lock writes our PID to the pid file if the file doesn't exist.
// Otherwise, it reads the PID file, and checks if a process with the PID exists.
// If so, it throws an error. Otherwise, if no such process is running, it writes our PID to the pid file.
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
		return fmt.Errorf("process with PID %d already running", pid)
	}
	return os.WriteFile(string(p), []byte(strconv.Itoa(currentPid)), 0644)
}
