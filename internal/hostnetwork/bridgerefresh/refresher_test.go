// SPDX-License-Identifier:Apache-2.0

//go:build runasroot
// +build runasroot

package bridgerefresh

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/hostnetwork"
)

var _ = Describe("BridgeRefresher", func() {
	const testNSName = "bridgerefreshtest"

	testNSPath := func() string {
		return fmt.Sprintf("/var/run/netns/%s", testNSName)
	}

	BeforeEach(func() {
		cleanTest(testNSName)
		createTestNS(testNSName)
	})

	AfterEach(func() {
		cleanTest(testNSName)
	})

	Describe("Start/Stop lifecycle", func() {
		It("should start and stop with valid params", func() {
			createTestBridge(testNSPath(), "br-pe-100")
			addIPToBridge(testNSPath(), "br-pe-100", "192.168.1.1/24")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      100,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher, err := Start(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher).NotTo(BeNil())
			Expect(refresher.bridgeName).To(Equal("br-pe-100"))
			Expect(refresher.gatewayIPs).To(HaveLen(1))

			refresher.Stop()
		})

		It("should start without gateway IPs and skip ARP", func() {
			createTestBridge(testNSPath(), "br-pe-101")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      101,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher, err := Start(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher.gatewayIPs).To(BeEmpty())

			refresher.Stop()
		})

		It("should filter out IPv6 gateway IPs", func() {
			createTestBridge(testNSPath(), "br-pe-102")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      102,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"2001:db8::1/64"},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher, err := Start(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher.gatewayIPs).To(BeEmpty())

			refresher.Stop()
		})

		It("should keep IPv4 and filter IPv6 in dual-stack", func() {
			createTestBridge(testNSPath(), "br-pe-103")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      103,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24", "2001:db8::1/64"},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher, err := Start(ctx, params)
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher.gatewayIPs).To(HaveLen(1))
			Expect(refresher.gatewayIPs[0].String()).To(Equal("192.168.1.1"))

			refresher.Stop()
		})

		It("should error on invalid gateway IP format", func() {
			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      104,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"not-an-ip"},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher, err := Start(ctx, params)
			Expect(err).To(HaveOccurred())
			Expect(refresher).To(BeNil())
		})

		It("should stop when context is cancelled", func() {
			createTestBridge(testNSPath(), "br-pe-105")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      105,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			ctx, cancel := context.WithCancel(context.Background())

			refresher, err := Start(ctx, params)
			Expect(err).NotTo(HaveOccurred())

			cancel()
			// Wait for goroutine to exit
			refresher.wg.Wait()
		})

		It("should use custom refresh period with StartWithOptions", func() {
			createTestBridge(testNSPath(), "br-pe-106")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      106,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			customPeriod := 5 * time.Second
			refresher, err := StartWithOptions(ctx, params, StartOptions{
				RefreshPeriod: customPeriod,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher.refreshPeriod).To(Equal(customPeriod))

			refresher.Stop()
		})

		It("should use default period when StartOptions.RefreshPeriod is zero", func() {
			createTestBridge(testNSPath(), "br-pe-107")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      107,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher, err := StartWithOptions(ctx, params, StartOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher.refreshPeriod).To(Equal(DefaultRefreshPeriod))

			refresher.Stop()
		})
	})

	Describe("parseGatewayIPs", func() {
		DescribeTable("should parse gateway IPs correctly",
			func(input []string, expectedLen int, expectedIPs []string, expectError bool) {
				ips, err := parseGatewayIPs(input)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(ips).To(HaveLen(expectedLen))
					for i, expectedIP := range expectedIPs {
						Expect(ips[i].String()).To(Equal(expectedIP))
					}
				}
			},
			Entry("empty input", []string{}, 0, []string{}, false),
			Entry("single IPv4", []string{"192.168.1.1/24"}, 1, []string{"192.168.1.1"}, false),
			Entry("single IPv6 filtered", []string{"2001:db8::1/64"}, 0, []string{}, false),
			Entry("multiple IPv4", []string{"192.168.1.1/24", "10.0.0.1/8"}, 2, []string{"192.168.1.1", "10.0.0.1"}, false),
			Entry("mixed IPv4 and IPv6", []string{"192.168.1.1/24", "2001:db8::1/64"}, 1, []string{"192.168.1.1"}, false),
			Entry("invalid CIDR", []string{"invalid"}, 0, nil, true),
			Entry("IP without mask", []string{"192.168.1.1"}, 0, nil, true),
		)
	})
})
