// SPDX-License-Identifier:Apache-2.0

package tlsconfig

import (
	"crypto/tls"
)

// DisableHTTP2 returns a TLS configuration function that disables HTTP/2.
// This is used to prevent vulnerabilities related to HTTP/2 Stream Cancellation
// and Rapid Reset CVEs. For more information see:
// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
// - https://github.com/advisories/GHSA-4374-p667-p6c8
func DisableHTTP2() func(*tls.Config) {
	return func(c *tls.Config) {
		c.NextProtos = []string{"http/1.1"}
	}
}
