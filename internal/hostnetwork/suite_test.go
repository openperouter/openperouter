// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package hostnetwork

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openperouter/openperouter/internal/netnamespace"
)

var _ = BeforeSuite(func() {
	Expect(netnamespace.InitHostNS("/proc/self/ns/net")).To(Succeed())
})

func TestAPIs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	RegisterFailHandler(Fail)

	RunSpecs(t, "HostNetwork Suite")
}
