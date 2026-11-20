// SPDX-License-Identifier:Apache-2.0

//go:build runasroot

package routerconfiguration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/api/v1alpha1"
	"github.com/openperouter/openperouter/internal/conversion"
	"github.com/openperouter/openperouter/internal/grout"
	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/vishvananda/netlink"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	groutTestImageEnv = "OPENPEROUTER_GROUT_TEST_IMAGE"
	groutSocketPath   = "/run/grout.sock"

	underlayDevice = "dummy0"
	underlayCIDR   = "192.0.2.0/24"
)

var sut *GroutDatapathConfigurator
var testTargetNS string

func TestGroutDatapathConfigurator(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping grout integration test in short mode")
	}
	slog.SetLogLoggerLevel(slog.LevelDebug)

	RegisterFailHandler(Fail)
	RunSpecs(t, "Grout Datapath Configurator Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	groutPID := startGroutContainer(ctx)
	useContainerGrcli(groutPID)
	testTargetNS = fmt.Sprintf("/proc/%d/ns/net", groutPID)

	sut = &GroutDatapathConfigurator{
		groutSocketPath:  groutSocketPath,
		deviceStateStore: devicestate.Store{Dir: GinkgoT().TempDir()},
		sysfs:            pci.Sysfs{Root: "/sys"},
		underlayPortMode: grout.PortModeTAP,
	}
})

var _ = Describe("GroutDatapathConfigurator", Serial, Ordered, func() {
	underlay := v1alpha1.Underlay{
		ObjectMeta: metav1.ObjectMeta{Name: "underlay"},
		Spec: v1alpha1.UnderlaySpec{
			ASN: 64514,
			Interfaces: []v1alpha1.UnderlayInterface{{
				Type:          v1alpha1.UnderlayInterfaceTypeNetworkDevice,
				NetworkDevice: &v1alpha1.NetworkDevice{InterfaceName: underlayDevice},
			}},
			TunnelEndpoint: &v1alpha1.TunnelEndpointConfig{
				CIDRs: []string{"198.51.100.0/24", "2001:db8::/64"},
			},
		},
	}
	l3VNI := v1alpha1.L3VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "red"},
		Spec: v1alpha1.L3VNISpec{
			VRF: "red", VNI: 100, VXLanPort: new(int32(4789)),
			HostSession: &v1alpha1.HostSession{
				ASN: 64514, HostASN: new(int64(64515)),
				LocalCIDRs: []string{"10.0.0.0/24", "2001:db8:100::/64"},
			},
		},
	}
	l3Passthrough := v1alpha1.L3Passthrough{
		ObjectMeta: metav1.ObjectMeta{Name: "passthrough"},
		Spec: v1alpha1.L3PassthroughSpec{
			HostSession: v1alpha1.HostSession{
				ASN: 64514, HostASN: new(int64(64516)),
				LocalCIDRs: []string{"203.0.113.0/24", "2001:db8:1::/64"},
			},
		},
	}
	standaloneL2VNI := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "standalone"},
		Spec: v1alpha1.L2VNISpec{
			VNI: 200, VXLanPort: new(int32(4789)),
			HostMaster: &v1alpha1.HostMaster{
				Type:        v1alpha1.LinuxBridge,
				LinuxBridge: &v1alpha1.LinuxBridgeConfig{Lifecycle: v1alpha1.BridgeLifecycleManaged},
			},
		},
	}
	routedL2VNI := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "routed"},
		Spec: v1alpha1.L2VNISpec{
			VNI: 210, VXLanPort: new(int32(4789)),
			RoutingDomain: &v1alpha1.RoutingDomain{
				Type:  v1alpha1.RoutingDomainTypeL3VNI,
				L3VNI: &v1alpha1.L3VNIReference{Name: l3VNI.Name},
			},
			GatewayIPs: []string{"192.168.10.1/24", "2001:db8:210::1/64"},
		},
	}
	ovsL2VNI := v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: "ovs"},
		Spec: v1alpha1.L2VNISpec{
			VNI: 220, VXLanPort: new(int32(4789)),
			HostMaster: &v1alpha1.HostMaster{
				Type:      v1alpha1.OVSBridge,
				OVSBridge: &v1alpha1.OVSBridgeConfig{Lifecycle: v1alpha1.BridgeLifecycleManaged},
			},
		},
	}

	configure := func(ctx context.Context, apiConfig conversion.APIConfigData) error {
		return sut.Configure(ctx, interfacesConfiguration{
			targetNamespace: testTargetNS,
			nodeIndex:       1,
			APIConfigData:   apiConfig,
		})
	}

	BeforeAll(func() {
		link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: underlayDevice}}
		Expect(netlink.LinkAdd(link)).To(Succeed())
		DeferCleanup(func() {
			link, err := netlink.LinkByName(underlayDevice)
			Expect(err).NotTo(HaveOccurred())
			Expect(netlink.LinkDel(link)).To(Succeed())
		})
		Expect(hostnetwork.AssignIPToInterface(link, underlayCIDR)).To(Succeed())
		Expect(netlink.LinkSetUp(link)).To(Succeed())
	})

	It("does nothing without an underlay", func(ctx SpecContext) {
		Expect(configure(ctx, conversion.APIConfigData{})).To(Succeed())

		Expect(groutInterfaceNames(ctx)).NotTo(ContainElement(grout.UnderlayPortName(underlayDevice)))
		Expect(hostAddresses(ctx, underlayDevice)).To(ContainSubstring(underlayCIDR))
	})

	It("configures the underlay, the L3VNI and the passthrough idempotently", func(ctx SpecContext) {
		apiConfig := conversion.APIConfigData{
			Underlays:     []v1alpha1.Underlay{underlay},
			L3VNIs:        []v1alpha1.L3VNI{l3VNI},
			L3Passthrough: []v1alpha1.L3Passthrough{l3Passthrough},
		}

		Expect(configure(ctx, apiConfig)).To(Succeed())

		Expect(groutAddresses(ctx, grout.UnderlayPortName(underlayDevice))).To(ContainElement(underlayCIDR))
		Expect(groutAddresses(ctx, hostnetwork.TunnelLoopback)).To(ContainElements("198.51.100.1/32", "2001:db8::1/128"))
		Expect(osexec.CommandContext(ctx, "nsenter", "--net="+testTargetNS, "ip", "route").CombinedOutput()).To(
			ContainSubstring("192.0.2.0/24 dev main src 192.0.2.0"),
		)

		Expect(groutInterface(ctx, "red")).To(HaveKeyWithValue("type", "vrf"))
		Expect(groutInterface(ctx, "vni100")).To(And(
			HaveKeyWithValue("vni", BeNumerically("==", 100)),
			HaveKeyWithValue("local", "198.51.100.1"),
			HaveKeyWithValue("dst_port", BeNumerically("==", 4789)),
			HaveKeyWithValue("vrf", "red"),
		))
		Expect(groutAddresses(ctx, "pe-e-100")).To(ContainElements("10.0.0.1/24", "2001:db8:100::1/64"))
		Expect(hostAddresses(ctx, "host-e-100")).To(And(
			ContainSubstring("10.0.0.3/24"),
			ContainSubstring("2001:db8:100::3/64"),
		))

		Expect(groutAddresses(ctx, "pt-ns")).To(ContainElements("203.0.113.1/24", "2001:db8:1::1/64"))
		Expect(hostAddresses(ctx, "pt-host")).To(And(
			ContainSubstring("203.0.113.3/24"),
			ContainSubstring("2001:db8:1::3/64"),
		))

		// Configure runs on every reconciliation.
		interfacesBefore := groutInterfaceNames(ctx)
		Expect(configure(ctx, apiConfig)).To(Succeed())
		Expect(groutInterfaceNames(ctx)).To(ConsistOf(interfacesBefore))
	})

	It("configures L2VNIs idempotently", func(ctx SpecContext) {
		apiConfig := conversion.APIConfigData{
			Underlays:     []v1alpha1.Underlay{underlay},
			L3VNIs:        []v1alpha1.L3VNI{l3VNI},
			L2VNIs:        []v1alpha1.L2VNI{standaloneL2VNI, routedL2VNI, ovsL2VNI},
			L3Passthrough: []v1alpha1.L3Passthrough{l3Passthrough},
		}

		Expect(configure(ctx, apiConfig)).To(Succeed())

		Expect(groutInterface(ctx, "vni200")).To(And(
			HaveKeyWithValue("vni", BeNumerically("==", 200)),
			HaveKeyWithValue("local", "198.51.100.1"),
			HaveKeyWithValue("domain", "br-pe-200"),
		))
		Expect(groutInterface(ctx, "br-pe-200")).To(And(
			HaveKeyWithValue("vrf", "main"),
			HaveKeyWithValue("bridge_members", ConsistOf("vni200", "pe-e-200")),
		))
		Expect(linkMaster("host-e-200")).To(Equal("br-hs-200"))

		Expect(groutInterface(ctx, "br-pe-210")).To(And(
			HaveKeyWithValue("vrf", "red"),
			HaveKeyWithValue("bridge_members", ConsistOf("vni210", "pe-e-210")),
		))
		Expect(groutAddresses(ctx, "br-pe-210")).To(ContainElements("192.168.10.1/24", "2001:db8:210::1/64"))
		Expect(linkMaster("host-e-210")).To(BeEmpty())

		Expect(groutInterface(ctx, "br-pe-220")).To(
			HaveKeyWithValue("bridge_members", ConsistOf("vni220", "pe-e-220")),
		)
		Expect(ovsBridgePorts(ctx, "br-hs-220")).To(ContainElement("host-e-220"))

		// Configure runs on every reconciliation.
		interfacesBefore := groutInterfaceNames(ctx)
		Expect(configure(ctx, apiConfig)).To(Succeed())
		Expect(groutInterfaceNames(ctx)).To(ConsistOf(interfacesBefore))
	})

	It("removes an L2VNI dropped from the configuration and keeps the others", func(ctx SpecContext) {
		Expect(configure(ctx, conversion.APIConfigData{
			Underlays:     []v1alpha1.Underlay{underlay},
			L3VNIs:        []v1alpha1.L3VNI{l3VNI},
			L2VNIs:        []v1alpha1.L2VNI{routedL2VNI, ovsL2VNI},
			L3Passthrough: []v1alpha1.L3Passthrough{l3Passthrough},
		})).To(Succeed())

		Expect(groutInterfaceNames(ctx)).To(And(
			Not(ContainElement("vni200")),
			Not(ContainElement("br-pe-200")),
			Not(ContainElement("pe-e-200")),
			ContainElements("vni210", "br-pe-210", "pe-e-210"),
			ContainElements("vni220", "br-pe-220", "pe-e-220"),
		))
		Expect(linkExists("host-e-200")).To(BeFalse())
		Expect(linkExists("br-hs-200")).To(BeFalse())
		Expect(ovsBridgePorts(ctx, "br-hs-220")).To(ContainElement("host-e-220"))
	})

	It("removes the VNIs and the passthrough dropped from the configuration", func(ctx SpecContext) {
		Expect(configure(ctx, conversion.APIConfigData{Underlays: []v1alpha1.Underlay{underlay}})).To(Succeed())

		Expect(groutInterfaceNames(ctx)).To(And(
			ContainElement(grout.UnderlayPortName(underlayDevice)),
			Not(ContainElement("vni100")),
			Not(ContainElement("red")),
			Not(ContainElement("pe-e-100")),
			Not(ContainElement("pt-ns")),
			Not(ContainElement("br-pe-210")),
			Not(ContainElement("br-pe-220")),
		))
		for _, device := range []string{"host-e-100", "pt-host", "host-e-210", "host-e-220", "br-hs-220"} {
			Expect(linkExists(device)).To(BeFalse(), "device %s", device)
		}
	})

	It("restores the underlay device when the underlay is removed", func(ctx SpecContext) {
		Expect(configure(ctx, conversion.APIConfigData{})).To(Succeed())

		Expect(groutInterfaceNames(ctx)).NotTo(Or(
			ContainElement(grout.UnderlayPortName(underlayDevice)),
			ContainElement(hostnetwork.TunnelLoopback),
		))
		Expect(hostAddresses(ctx, underlayDevice)).To(ContainSubstring(underlayCIDR))
		Expect(grout.UnderlayInterfaces(sut.deviceStateStore)).To(BeEmpty())
	})
})

// startGroutContainer runs a grout daemon and returns its PID on the host.
func startGroutContainer(ctx context.Context) int {
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

	inspect, err := groutContainer.Inspect(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(inspect.State.Pid).NotTo(BeZero())
	return inspect.State.Pid
}

// useContainerGrcli makes the configurator execute the real grcli binary
// beside the grout daemon. The configurator builds its own grout client, so
// grcli is redirected through PATH: the shim enters only the mount namespace
// of the daemon, which keeps the test on the host (where it can move netdevs
// between namespaces) without mocking grout responses.
func useContainerGrcli(groutPID int) {
	GinkgoHelper()
	shimDir := GinkgoT().TempDir()
	shim := fmt.Sprintf("#!/bin/sh\nexec nsenter --mount=/proc/%d/ns/mnt grcli \"$@\"\n", groutPID)
	Expect(os.WriteFile(filepath.Join(shimDir, "grcli"), []byte(shim), 0755)).To(Succeed())

	originalPath := os.Getenv("PATH")
	Expect(os.Setenv("PATH", shimDir+string(os.PathListSeparator)+originalPath)).To(Succeed())
	DeferCleanup(func() {
		Expect(os.Setenv("PATH", originalPath)).To(Succeed())
	})
}

func groutInterfaceNames(ctx context.Context) []string {
	GinkgoHelper()
	var ifaces []struct {
		Name string `json:"name"`
	}
	Expect(json.Unmarshal(grcli(ctx, "interface", "show"), &ifaces)).To(Succeed())
	names := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		names = append(names, iface.Name)
	}
	return names
}

func groutInterface(ctx context.Context, name string) map[string]any {
	GinkgoHelper()
	var iface map[string]any
	Expect(json.Unmarshal(grcli(ctx, "interface", "show", "name", name), &iface)).To(Succeed())
	return iface
}

func groutAddresses(ctx context.Context, iface string) []string {
	GinkgoHelper()
	var entries []struct {
		Address string `json:"address"`
	}
	Expect(json.Unmarshal(grcli(ctx, "address", "show", "iface", iface), &entries)).To(Succeed())
	addresses := make([]string, 0, len(entries))
	for _, entry := range entries {
		addresses = append(addresses, entry.Address)
	}
	return addresses
}

func linkExists(name string) bool {
	GinkgoHelper()
	_, err := netlink.LinkByName(name)
	if errors.As(err, &netlink.LinkNotFoundError{}) {
		return false
	}
	Expect(err).NotTo(HaveOccurred())
	return true
}

// linkMaster returns the name of the device the given link is enslaved to,
// empty when it has none.
func linkMaster(name string) string {
	GinkgoHelper()
	link, err := netlink.LinkByName(name)
	Expect(err).NotTo(HaveOccurred())
	if link.Attrs().MasterIndex == 0 {
		return ""
	}
	master, err := netlink.LinkByIndex(link.Attrs().MasterIndex)
	Expect(err).NotTo(HaveOccurred())
	return master.Attrs().Name
}

func ovsBridgePorts(ctx context.Context, bridge string) []string {
	GinkgoHelper()
	out, err := osexec.CommandContext(ctx, "ovs-vsctl", "list-ports", bridge).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "ovs-vsctl list-ports %s: %s", bridge, out)
	return strings.Fields(string(out))
}

func grcli(ctx context.Context, args ...string) []byte {
	GinkgoHelper()
	args = append([]string{"--err-exit", "--json", "--socket", groutSocketPath}, args...)
	out, err := osexec.CommandContext(ctx, "grcli", args...).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "grcli %v: %s", args, out)
	return out
}

func hostAddresses(ctx context.Context, device string) string {
	GinkgoHelper()
	out, err := osexec.CommandContext(ctx, "ip", "-o", "addr", "show", "dev", device).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "ip addr show dev %s: %s", device, out)
	return string(out)
}
