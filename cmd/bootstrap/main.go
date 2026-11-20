// SPDX-License-Identifier:Apache-2.0

// Command bootstrap renders the systemd Quadlets and configuration of a
// hostmode OpenPERouter deployment. It is run by the
// openperouter-bootstrap.service unit at every boot and only writes files.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openperouter/openperouter/internal/bootstrap"
)

func main() {
	configPath := flag.String("config", "/etc/openperouter/openpe_boot.yaml", "host path of the boot configuration")
	root := flag.String("root", "/", "directory where the host filesystem is mounted")
	flag.Parse()

	data, err := os.ReadFile(filepath.Join(*root, *configPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	deployment, err := bootstrap.ParseConfig(data)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	files, err := bootstrap.Render(deployment)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := bootstrap.Write(*root, files); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("rendered %d files\n", len(files))
}
