// SPDX-License-Identifier:Apache-2.0

package frrconfig

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

func UpdaterForAddress(address string, configFile string) func(context.Context, string) error {
	return func(ctx context.Context, config string) error {
		slog.InfoContext(ctx, "updater writing frr file", "file", configFile)
		err := os.WriteFile(configFile, []byte(config), 0600)
		if err != nil {
			return fmt.Errorf("failed to write the config to %s", configFile)
		}
		requestURL := fmt.Sprintf("http://%s", address)
		slog.InfoContext(ctx, "updater requesting update", "url", requestURL)
		defer slog.InfoContext(ctx, "updater update requested")
		res, err := http.Post(requestURL, "", nil) // #nosec G107
		if err != nil {
			return fmt.Errorf("failed to reload against %s: %w", address, err)
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to reload against %s, status %d", address, res.StatusCode)
		}
		return nil
	}
}
