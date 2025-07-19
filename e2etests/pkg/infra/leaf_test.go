// SPDX-License-Identifier:Apache-2.0

package infra

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLeaf(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Leaf Suite")
}

var _ = Describe("LeafKind Configuration", func() {
	It("should generate configuration without BFD", func() {
		config := LeafKindConfiguration{
			EnableBFD: false,
		}

		result, err := LeafKindConfigToFRR(config)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(ContainSubstring("neighbor 192.168.1.4 remote-as 64612"))
		Expect(result).NotTo(ContainSubstring("neighbor kind-nodes bfd"))
	})

	It("should generate configuration with BFD", func() {
		config := LeafKindConfiguration{
			EnableBFD: true,
		}

		result, err := LeafKindConfigToFRR(config)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(ContainSubstring("neighbor 192.168.1.4 remote-as 64612"))
		Expect(result).To(ContainSubstring("neighbor kind-nodes bfd"))
	})

	It("should include debug BFD commands", func() {
		config := LeafKindConfiguration{
			EnableBFD: false,
		}

		result, err := LeafKindConfigToFRR(config)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(ContainSubstring("debug bfd network"))
		Expect(result).To(ContainSubstring("debug bfd peer"))
		Expect(result).To(ContainSubstring("debug bfd zebra"))
	})
})
