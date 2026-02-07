// SPDX-License-Identifier:Apache-2.0

//go:build runasroot
// +build runasroot

package bridgerefresh

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

var _ = Describe("Registry", func() {
	const testNSName = "registrytest"

	testNSPath := func() string {
		return fmt.Sprintf("/var/run/netns/%s", testNSName)
	}

	BeforeEach(func() {
		createTestNS(testNSName)
	})

	AfterEach(func() {
		StopAll()
		cleanTest(testNSName)
	})

	Describe("StartForVNI", func() {
		It("should start refresher for a single VNI", func() {
			createTestBridge(testNSPath(), "br-pe-200")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      200,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			err := StartForVNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(1))
		})

		It("should replace existing refresher for same VNI", func() {
			createTestBridge(testNSPath(), "br-pe-201")
			addIPToBridge(testNSPath(), "br-pe-201", "192.168.1.1/24")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      201,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			err := StartForVNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(1))

			// Start again with different gateway IP - should replace
			params.L2GatewayIPs = []string{"192.168.2.1/24"}
			err = StartForVNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(1))
		})

		It("should support multiple VNIs", func() {
			createTestBridge(testNSPath(), "br-pe-202")
			createTestBridge(testNSPath(), "br-pe-203")

			params1 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      202,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}
			params2 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      203,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.2.1/24"},
			}

			err := StartForVNI(context.Background(), params1)
			Expect(err).NotTo(HaveOccurred())

			err = StartForVNI(context.Background(), params2)
			Expect(err).NotTo(HaveOccurred())

			Expect(ActiveCount()).To(Equal(2))
		})
	})

	Describe("StopForVNI", func() {
		It("should stop specific VNI", func() {
			createTestBridge(testNSPath(), "br-pe-204")
			createTestBridge(testNSPath(), "br-pe-205")

			params1 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      204,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}
			params2 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      205,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.2.1/24"},
			}

			err := StartForVNI(context.Background(), params1)
			Expect(err).NotTo(HaveOccurred())

			err = StartForVNI(context.Background(), params2)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(2))

			err = StopForVNI(204)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(1))
		})

		It("should be no-op for non-existent VNI", func() {
			Expect(ActiveCount()).To(Equal(0))

			err := StopForVNI(999)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(0))
		})
	})

	Describe("StopAll", func() {
		It("should stop all active refreshers", func() {
			createTestBridge(testNSPath(), "br-pe-206")
			createTestBridge(testNSPath(), "br-pe-207")

			params1 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      206,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}
			params2 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      207,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.2.1/24"},
			}

			err := StartForVNI(context.Background(), params1)
			Expect(err).NotTo(HaveOccurred())

			err = StartForVNI(context.Background(), params2)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(2))

			StopAll()
			Expect(ActiveCount()).To(Equal(0))
		})
	})

	Describe("StopForRemovedVNIs", func() {
		It("should stop refreshers for VNIs no longer configured", func() {
			createTestBridge(testNSPath(), "br-pe-208")
			createTestBridge(testNSPath(), "br-pe-209")
			createTestBridge(testNSPath(), "br-pe-210")

			params1 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      208,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}
			params2 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      209,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.2.1/24"},
			}
			params3 := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      210,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.3.1/24"},
			}

			err := StartForVNI(context.Background(), params1)
			Expect(err).NotTo(HaveOccurred())

			err = StartForVNI(context.Background(), params2)
			Expect(err).NotTo(HaveOccurred())

			err = StartForVNI(context.Background(), params3)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(3))

			// Only keep params1, remove params2 and params3
			StopForRemovedVNIs([]hostnetwork.L2VNIParams{params1})
			Expect(ActiveCount()).To(Equal(1))
		})

		It("should handle empty configured list", func() {
			createTestBridge(testNSPath(), "br-pe-211")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      211,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			err := StartForVNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(1))

			StopForRemovedVNIs([]hostnetwork.L2VNIParams{})
			Expect(ActiveCount()).To(Equal(0))
		})
	})

	Describe("ActiveCount", func() {
		It("should return correct count", func() {
			Expect(ActiveCount()).To(Equal(0))

			createTestBridge(testNSPath(), "br-pe-212")
			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      212,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			err := StartForVNI(context.Background(), params)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(1))

			err = StopForVNI(212)
			Expect(err).NotTo(HaveOccurred())
			Expect(ActiveCount()).To(Equal(0))
		})
	})
})
