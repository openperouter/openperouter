// SPDX-License-Identifier:Apache-2.0

package sysctl

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSysctl(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sysctl Suite")
}
