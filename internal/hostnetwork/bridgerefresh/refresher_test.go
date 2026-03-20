// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

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
		createTestNS(testNSName)
	})

	AfterEach(func() {
		cleanTest(testNSName)
	})

	Describe("New/Start/Stop lifecycle", func() {
		It("should create and start with valid params", func() {
			createTestBridge(testNSPath(), "br-pe-100")
			addIPToBridge(testNSPath(), "br-pe-100", "192.168.1.1/24")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      100,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			refresher, err := New(params, StartOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher).NotTo(BeNil())
			Expect(refresher.bridgeName).To(Equal("br-pe-100"))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher.Start(ctx)
			refresher.Stop()
		})

		It("should create without gateway IPs", func() {
			createTestBridge(testNSPath(), "br-pe-101")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      101,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{},
			}

			refresher, err := New(params, StartOptions{})
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher.Start(ctx)
			refresher.Stop()
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

			refresher, err := New(params, StartOptions{})
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithCancel(context.Background())
			refresher.Start(ctx)

			cancel()
			// Wait for goroutine to exit
			refresher.wg.Wait()
		})

		It("should use custom refresh period from StartOptions", func() {
			createTestBridge(testNSPath(), "br-pe-106")

			params := hostnetwork.L2VNIParams{
				VNIParams: hostnetwork.VNIParams{
					VNI:      106,
					TargetNS: testNSPath(),
				},
				L2GatewayIPs: []string{"192.168.1.1/24"},
			}

			customPeriod := 5 * time.Second
			refresher, err := New(params, StartOptions{
				RefreshPeriod: customPeriod,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher.refreshPeriod).To(Equal(customPeriod))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher.Start(ctx)
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

			refresher, err := New(params, StartOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(refresher.refreshPeriod).To(Equal(DefaultRefreshPeriod))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			refresher.Start(ctx)
			refresher.Stop()
		})
	})
})
