// SPDX-License-Identifier:Apache-2.0

package hostcredentials

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"
)

func resolveKubernetesServiceIP() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	targets := []string{
		"kubernetes.default.svc.cluster.local",
		"kubernetes.default.svc",
		"kubernetes.default",
		"kubernetes",
	}

	for _, target := range targets {
		ip, err := resolveName(ctx, target)
		if err != nil {
			slog.Warn("Failed to resolve target", "target", target, "error", err)
			continue
		}
		return ip, nil
	}

	return "", fmt.Errorf("could not get kubernetes apiserver ip")
}

func resolveName(ctx context.Context, hostname string) (string, error) {
	slog.Debug("Attempting to resolve hostname", "hostname", hostname)

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		slog.Warn("Failed to resolve hostname", "hostname", hostname, "error", err)
		return "", fmt.Errorf("failed to resolve %s: %w", hostname, err)
	}

	if len(ips) == 0 {
		slog.Warn("No IPs found for hostname", "hostname", hostname)
		return "", fmt.Errorf("no IPs found for %s", hostname)
	}

	for _, ip := range ips {
		if ip.IP.To4() != nil {
			slog.Info("Resolved hostname to IPv4", "hostname", hostname, "ip", ip.IP.String())
			return ip.IP.String(), nil
		}
	}

	slog.Info("Resolved hostname to IP", "hostname", hostname, "ip", ips[0].IP.String())
	return ips[0].IP.String(), nil
}
