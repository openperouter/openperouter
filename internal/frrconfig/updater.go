// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
)

func UpdaterForSocket(socketPath, configFile string) func(context.Context, string) error {
	return func(ctx context.Context, config string) error {
		updaterClient := func(ctx context.Context) error {
			client := &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
						return net.Dial("unix", socketPath)
					},
				},
			}

			slog.InfoContext(ctx, "updater requesting update", "socket", socketPath)
			defer slog.InfoContext(ctx, "updater update requested")

			res, err := client.Post("http://unix/", "", nil)
			if err != nil {
				return fmt.Errorf("failed to reload against socket %s: %w", socketPath, err)
			}
			defer func() {
				if err := res.Body.Close(); err != nil {
					slog.ErrorContext(ctx, "failed to close res body", "error", err)
				}
			}()
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("failed to reload against socket %s, status %d", socketPath, res.StatusCode)
			}
			return nil
		}

		slog.InfoContext(ctx, "updater writing frr file", "file", configFile)
		err := os.WriteFile(configFile, []byte(config), 0600)
		if err != nil {
			return fmt.Errorf("failed to write the config to %s", configFile)
		}
		return updaterClient(ctx)
	}
}
