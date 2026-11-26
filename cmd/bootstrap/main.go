// SPDX-License-Identifier:Apache-2.0

// Command bootstrap renders the systemd Quadlets and configuration of a
// hostmode OpenPERouter deployment. It is run by the
// openperouter-bootstrap.service unit at every boot and only writes files.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/openperouter/openperouter/internal/bootstrap"
)

func main() {
	root := flag.String("root", "/", "directory where the host filesystem is mounted")
	flag.Parse()

	if err := run(*root); err != nil {
		slog.Error("bootstrap failed", "error", err)
		os.Exit(1)
	}
}

func run(rootPath string) error {
	const configPath = "var/lib/openperouter/boot.yaml"
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return fmt.Errorf("open host root: %w", err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			slog.Warn("failed to close host root", "error", err)
		}
	}()

	data, err := root.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read boot configuration: %w", err)
	}
	deployment, err := bootstrap.ParseConfig(data)
	if err != nil {
		return err
	}
	files, err := bootstrap.Render(deployment)
	if err != nil {
		return err
	}
	if err := bootstrap.Write(root, files); err != nil {
		return err
	}
	for _, file := range files {
		slog.Info("rendered file", "path", file.Path)
	}
	return nil
}
