// SPDX-License-Identifier:Apache-2.0

package url

import (
	"fmt"
	"net"
	"strings"
)

// Format formats a URL string with the given IP address, properly handling IPv6
// addresses by enclosing them in square brackets when used in URLs.
func Format(format, ip string) string {
	// Check if the IP is IPv6
	if strings.Contains(ip, ":") && !strings.Contains(ip, ".") {
		// Validate it's actually an IPv6 address
		if parsedIP := net.ParseIP(ip); parsedIP != nil && parsedIP.To4() == nil {
			// Enclose IPv6 address in square brackets for URL formatting
			return fmt.Sprintf(format, "["+ip+"]")
		}
	}

	// For IPv4 or invalid addresses, use as-is
	return fmt.Sprintf(format, ip)
}
