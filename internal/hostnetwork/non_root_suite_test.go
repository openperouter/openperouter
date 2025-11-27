// SPDX-License-Identifier:Apache-2.0

//go:build !runasroot
// +build !runasroot

package hostnetwork

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNonRootUnitTests(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "HostNetwork Non-Root Unit Suite")
}
