// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package grout

import (
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"testing"

	"github.com/moby/moby/api/types/container"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/vishvananda/netlink"
)

const groutTestImageEnv = "OPENPEROUTER_GROUT_TEST_IMAGE"

var _ = Describe("Underlay setup", Serial, func() {
	It("configures the kernel and grout underlays idempotently", func(ctx SpecContext) {
		groutContainer := startGroutContainer(ctx)
		useContainerGrcli(groutContainer)
		targetNS := getContainerNetworkNS(ctx, groutContainer)

		underlayName := "dummy0"
		underlayCIDR := "192.0.2.0/24"
		underlay := createDummyNetLink(underlayName, underlayCIDR)
		DeferCleanup(func() {
			link, err := netlink.LinkByName(underlay.Attrs().Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(netlink.LinkDel(link)).To(Succeed())
		})

		params := hostnetwork.UnderlayParams{
			TargetNS: targetNS,
			UnderlayInterfaces: []hostnetwork.UnderlayInterface{{
				InterfaceName: underlayName,
				Kind:          hostnetwork.UnderlayInterfaceNetDev,
			}},
			TunnelEndpoint: &hostnetwork.UnderlayTunnelEndpointParams{
				IPv4CIDR: "198.51.100.0/24",
				IPv6CIDR: "2001:db8::0/64",
			},
		}
		groutClient := NewClient("/run/grout.sock")
		DeferCleanup(func(ctx SpecContext) {
			Expect(RestoreUnderlay(ctx, groutClient, targetNS, params.UnderlayInterfaces)).To(Succeed())
		})

		Expect(SetupUnderlay(ctx, groutClient, params)).To(Succeed())

		Expect(groutClient.portExists(ctx, "u_dummy0")).To(BeTrue())

		Expect(groutClient.getAddresses(ctx, "u_dummy0")).To(
			ContainElement("192.0.2.0/24"),
		)

		Expect(groutClient.getAddresses(ctx, defaultVRFName)).To(
			And(
				ContainElement("198.51.100.0/24"),
				ContainElement("2001:db8::/64"),
			),
		)

		Expect(osexec.CommandContext(ctx, "nsenter", "--net="+targetNS, "ip", "route").CombinedOutput()).To(
			ContainSubstring("192.0.2.0/24 dev main src 192.0.2.0"),
		)

		// SetupUnderlay must be safe to call on every reconciliation.
		Expect(SetupUnderlay(ctx, groutClient, params)).To(Succeed())
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
		//time.Sleep(1000 * time.Second)
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

func TestGroutIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping grout integration test in short mode")
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Grout Suite")
}
