// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"fmt"
	"slices"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"github.com/vishvananda/netns"
)

func TestVLANNames(t *testing.T) {
	for _, tt := range []struct {
		vni                   int32
		bridge, endpoint, svi string
	}{
		{vni: 5000, bridge: "br-vl-5000", endpoint: "br-pe-5000", svi: "svi5000"},
		{vni: 16777215, bridge: "br-vl-16777215", endpoint: "br-pe-16777215", svi: "svi16777215"},
	} {
		t.Run(fmt.Sprint(tt.vni), func(t *testing.T) {
			got := []string{vlanBridgeName(tt.vni), BridgeName(tt.vni), vlanSVIName(tt.vni)}
			want := []string{tt.bridge, tt.endpoint, tt.svi}
			if !slices.Equal(got, want) {
				t.Fatalf("VNI names = %v, want %v", got, want)
			}
			for _, name := range got {
				if len(name) > 15 {
					t.Fatalf("interface name %q exceeds kernel limit", name)
				}
			}
		})
	}
}

func TestExpandedVLANMemberships(t *testing.T) {
	entries := []*nl.BridgeVlanInfo{
		{Vid: 1, Flags: nl.BRIDGE_VLAN_INFO_PVID | nl.BRIDGE_VLAN_INFO_UNTAGGED},
		{Vid: 2, Flags: nl.BRIDGE_VLAN_INFO_RANGE_BEGIN},
		{Vid: 4, Flags: nl.BRIDGE_VLAN_INFO_RANGE_END},
	}
	expanded := expandedVLANMemberships(entries)
	got := make([]uint16, 0, len(expanded))
	for _, entry := range expanded {
		got = append(got, entry.Vid)
	}
	if want := []uint16{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Fatalf("expanded VLANs = %v, want %v", got, want)
	}
	if !expanded[0].PortVID() || !expanded[0].EngressUntag() {
		t.Fatalf("VLAN 1 flags changed: %+v", expanded[0])
	}
}

var _ = Describe("VLAN-aware VNI endpoints", func() {
	var ns netns.NsHandle
	BeforeEach(func() { ns = createTestNS("vlantestns") })
	AfterEach(func() {
		cleanTest("vlantestns")
		Expect(ns.Close()).To(Succeed())
	})

	It("uses separate VLAN 1 domains and addressless VRF endpoints for full-width VNIs", func() {
		Expect(netnamespace.In(ns, func() error {
			Expect(setupVRF("testvrf")).To(Succeed())
			vrf, err := lookupVRF("testvrf")
			Expect(err).NotTo(HaveOccurred())
			for _, vni := range []int32{5000, 16777215} {
				params := VNIParams{VNI: vni, VRF: "testvrf"}
				bridge, err := setupBridge(params)
				Expect(err).NotTo(HaveOccurred())
				actual, err := netlink.LinkByIndex(bridge.Index)
				Expect(err).NotTo(HaveOccurred())
				observed := actual.(*netlink.Bridge)
				Expect(observed.Name).To(Equal(fmt.Sprintf("br-vl-%d", vni)))
				Expect(observed.MasterIndex).To(BeZero())
				Expect(*observed.VlanFiltering).To(BeTrue())
				Expect(*observed.VlanDefaultPVID).To(BeZero())
				membership, err := netlink.BridgeVlanList()
				Expect(err).NotTo(HaveOccurred())
				Expect(membership[int32(bridge.Index)]).To(HaveLen(1))
				Expect(membership[int32(bridge.Index)][0].Vid).To(Equal(uint16(1)))
				endpoint, err := setupVNIEndpoint(params, bridge)
				Expect(err).NotTo(HaveOccurred())
				vlan := endpoint.(*netlink.Vlan)
				Expect(vlan.Name).To(Equal(BridgeName(vni)))
				Expect(vlan.VlanId).To(Equal(1))
				Expect(vlan.ParentIndex).To(Equal(bridge.Index))
				Expect(netlink.LinkByName(vlan.Name)).To(HaveField("LinkAttrs.MasterIndex", vrf.Index))
				Expect(netlink.AddrList(endpoint, netlink.FAMILY_ALL)).To(BeEmpty())
				Expect(vlan.HardwareAddr).To(Equal(bridge.HardwareAddr))
				index := vlan.Index
				repeated, err := setupVNIEndpoint(params, bridge)
				Expect(err).NotTo(HaveOccurred())
				Expect(repeated.Attrs().Index).To(Equal(index))
			}
			return nil
		})).To(Succeed())
	})

	It("repairs explicit access membership without adding a second VLAN", func() {
		Expect(netnamespace.In(ns, func() error {
			bridge, err := setupBridge(VNIParams{VNI: 5000})
			Expect(err).NotTo(HaveOccurred())
			port := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "access"}}
			Expect(netlink.LinkAdd(port)).To(Succeed())
			Expect(netlink.LinkSetMaster(port, bridge)).To(Succeed())
			Expect(ensureAccessVLAN(port)).To(Succeed())
			Expect(netlink.BridgeVlanAddRange(port, 2, 4, false, false, false, false)).To(Succeed())
			Expect(ensureAccessVLAN(port)).To(Succeed())
			Expect(netlink.BridgeVlanDel(port, 1, true, true, false, false)).To(Succeed())
			Expect(ensureAccessVLAN(port)).To(Succeed())
			membership, err := netlink.BridgeVlanList()
			Expect(err).NotTo(HaveOccurred())
			Expect(membership[int32(port.Index)]).To(HaveLen(1))
			entry := membership[int32(port.Index)][0]
			Expect(entry.Vid).To(Equal(uint16(1)))
			Expect(entry.PortVID()).To(BeTrue())
			Expect(entry.EngressUntag()).To(BeTrue())
			return nil
		})).To(Succeed())
	})
})
