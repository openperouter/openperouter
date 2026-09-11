// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package grout

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	osexec "os/exec"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/vishvananda/netlink"
)

const groutTestImageEnv = "OPENPEROUTER_GROUT_TEST_IMAGE"

var testGroutClient *Client
var testTargetNS string

func TestGroutIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping grout integration test in short mode")
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)

	RegisterFailHandler(Fail)
	RunSpecs(t, "Grout Suite")
}

var _ = ginkgo.BeforeSuite(func(ctx context.Context) {
	groutContainer := startGroutContainer(ctx)
	useContainerGrcli(groutContainer)
	testTargetNS = getContainerNetworkNS(ctx, groutContainer)

	testGroutClient = NewClient("/run/grout.sock")
})

var _ = Describe("Datapath", Serial, Ordered, func() {

	Context("Underlay", func() {
		It("configures grout underlay idempotently", func(ctx SpecContext) {
			underlay := createDummyNetLink("dummy0", "192.0.2.0/24")
			DeferCleanup(func() {
				link, err := netlink.LinkByName(underlay.Attrs().Name)
				Expect(err).NotTo(HaveOccurred())
				Expect(netlink.LinkDel(link)).To(Succeed())
			})

			params := hostnetwork.UnderlayParams{
				TargetNS: testTargetNS,
				UnderlayInterfaces: []hostnetwork.UnderlayInterface{{
					InterfaceName: "dummy0",
					Kind:          hostnetwork.UnderlayInterfaceNetDev,
				}},
				TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
					IPv4CIDR: "198.51.100.1/24",
					IPv6CIDR: "2001:db8::0/64",
				},
			}

			Expect(SetupUnderlay(ctx, testGroutClient, params)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				Expect(RestoreUnderlay(ctx, testGroutClient, testTargetNS, params.UnderlayInterfaces)).To(Succeed())
			})
			Expect(testGroutClient.portExists(ctx, "u_dummy0")).To(BeTrue())
			Expect(testGroutClient.getAddresses(ctx, "u_dummy0")).To(ContainElement("192.0.2.0/24"))
			Expect(testGroutClient.getAddresses(ctx, defaultVRFName)).To(
				And(
					ContainElement("198.51.100.1/24"),
					ContainElement("2001:db8::/64"),
				),
			)

			Expect(osexec.CommandContext(ctx, "nsenter", "--net="+testTargetNS, "ip", "route").CombinedOutput()).To(
				ContainSubstring("192.0.2.0/24 dev main src 192.0.2.0"),
			)

			// SetupUnderlay must be safe to call on every reconciliation.
			Expect(SetupUnderlay(ctx, testGroutClient, params)).To(Succeed())
		})
	})
	Context("L3Passthrough", func() {

		It("configures grout passthrough idempotently", func(ctx SpecContext) {
			params := hostnetwork.PassthroughParams{
				TargetNS: testTargetNS,
				LinkIPs: hostnetwork.LinkIPs{
					HostIPv4: "203.0.113.2/24", NSIPv4: "203.0.113.1/24",
					HostIPv6: "2001:db8:1::2/64", NSIPv6: "2001:db8:1::1/64",
				},
			}
			DeferCleanup(func(ctx SpecContext) {
				Expect(RemovePassthrough(ctx, testGroutClient)).To(Succeed())
			})

			Expect(SetupPassthrough(ctx, testGroutClient, params)).To(Succeed())
			Expect(testGroutClient.portExists(ctx, "pt-ns")).To(BeTrue())
			Expect(testGroutClient.getAddresses(ctx, "pt-ns")).To(And(
				ContainElement(params.LinkIPs.NSIPv4),
				ContainElement(params.LinkIPs.NSIPv6),
			))
			Expect(osexec.CommandContext(ctx, "ip", "-o", "addr", "show", "dev", "pt-host").CombinedOutput()).To(And(
				ContainSubstring(params.LinkIPs.HostIPv4),
				ContainSubstring(params.LinkIPs.HostIPv6),
			))

			Expect(SetupPassthrough(ctx, testGroutClient, params)).To(Succeed())
		})
	})

	Context("L3VNI", func() {
		It("configures grout L3VNI idempotently", func(ctx SpecContext) {
			params := hostnetwork.L3VNIParams{
				VNIParams: hostnetwork.VNIParams{
					TargetNS: testTargetNS, VRF: "red", VNI: 100, VTEPIP: "198.51.100.1/24",
				},
				LinkIPs: &hostnetwork.LinkIPs{
					HostIPv4: "10.0.0.2/24", NSIPv4: "10.0.0.1/24",
					HostIPv6: "2001:db8:100::2/64", NSIPv6: "2001:db8:100::1/64",
				},
			}

			Expect(SetupL3VNI(ctx, testGroutClient, params)).To(Succeed())
			DeferCleanup(func(ctx SpecContext) {
				Expect(removeVNI(ctx, testGroutClient, params.VNI)).To(Succeed())
			})

			Expect(testGroutClient.getInterfaceInfo(ctx, params.VRF)).To(Equal(&groutInterface{Name: "red", Type: "vrf"}))
			Expect(testGroutClient.getVXLANInterfaceInfo(ctx, "vni100")).To(Equal(&groutVXLANInfo{
				VNI: 100, Local: "198.51.100.1", DstPort: 4789, VRF: "red",
			}))
			Expect(testGroutClient.getAddresses(ctx, "pe-100")).To(And(
				ContainElement(params.LinkIPs.NSIPv4),
				ContainElement(params.LinkIPs.NSIPv6),
			))
			Expect(osexec.CommandContext(ctx, "ip", "-o", "addr", "show", "dev", "host-100").CombinedOutput()).To(And(
				ContainSubstring(params.LinkIPs.HostIPv4),
				ContainSubstring(params.LinkIPs.HostIPv6),
			))

			Expect(SetupL3VNI(ctx, testGroutClient, params)).To(Succeed())
		})
	})
})

func startGroutContainer(ctx context.Context) testcontainers.Container {
	GinkgoHelper()
	image := os.Getenv(groutTestImageEnv)
	if image == "" {
		image = "quay.io/grout/grout:0.17.1"
	}

	groutContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:      image,
			Entrypoint: []string{"grout"},
			Cmd:        []string{"--test-mode", "--socket-mode", "0666"},
			HostConfigModifier: func(hostConfig *container.HostConfig) {
				hostConfig.Privileged = true
			},
			WaitingFor: wait.ForExec([]string{"grcli", "-e"}),
			Env: map[string]string{
				"GROUT_FIB4_ALGORITHM": "DUMMY",
				"GROUT_FIB6_ALGORITHM": "DUMMY",
			},
		},
		Started: true,
	})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func(ctx SpecContext) {
		Expect(groutContainer.Terminate(ctx)).To(Succeed())
	})
	return groutContainer
}

// useContainerGrcli makes Client execute the real grcli binary beside the
// grout daemon. This keeps the test on the host (where it can move netdevs
// between namespaces) without mocking grout responses.
func useContainerGrcli(groutContainer testcontainers.Container) {
	GinkgoHelper()
	originalExecCmd := execCmd
	execCmd = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "grcli" {
			return nil, fmt.Errorf("unexpected command %q", name)
		}
		exitCode, reader, err := groutContainer.Exec(ctx, append([]string{name}, args...), exec.Multiplexed())
		if err != nil {
			return nil, err
		}
		output, readErr := io.ReadAll(reader)
		if readErr != nil {
			return nil, readErr
		}
		if exitCode != 0 {
			return output, fmt.Errorf("grcli exited with status %d", exitCode)
		}
		return output, nil
	}
	DeferCleanup(func() { execCmd = originalExecCmd })
}

func createDummyNetLink(name, cidr string) netlink.Link {
	GinkgoHelper()
	link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}
	Expect(netlink.LinkAdd(link)).To(Succeed())
	Expect(hostnetwork.AssignIPToInterface(link, cidr)).To(Succeed())
	Expect(netlink.LinkSetUp(link)).To(Succeed())
	return link
}

func getContainerNetworkNS(ctx context.Context, groutContainer testcontainers.Container) string {
	GinkgoHelper()
	inspect, err := groutContainer.Inspect(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(inspect.State.Pid).NotTo(BeZero())
	return fmt.Sprintf("/proc/%d/ns/net", inspect.State.Pid)
}
