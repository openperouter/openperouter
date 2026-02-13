// SPDX-License-Identifier: Apache-2.0

package bpf

import (
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Manager wraps the bpf2go-generated eBPF objects for the shared NIC mode.
// It loads and attaches TC programs to the physical NIC and the ul-host veth,
// and provides methods to update the BPF maps at runtime.
//
// Attachment uses TCX when the kernel supports it (>= 6.6) and falls back
// to legacy TC cls_bpf on older kernels (e.g. RHEL 5.14).
type Manager struct {
	nicObjs NicIngressObjects
	ulObjs  UlHostIngressObjects

	// TCX links (nil when using legacy TC)
	nicLink link.Link
	ulLink  link.Link

	// Legacy TC state (used when TCX is not available)
	legacyTC       bool
	nicIfindex     int
	ulHostIfindex  int
	nicFilterProto uint16
	ulFilterProto  uint16
}

// NewManager loads the eBPF programs and attaches them to the specified interfaces.
// nicIfindex is the ifindex of the physical NIC.
// ulHostIfindex is the ifindex of the ul-host veth (host side of the underlay veth pair).
func NewManager(nicIfindex, ulHostIfindex int) (*Manager, error) {
	m := &Manager{
		nicIfindex:    nicIfindex,
		ulHostIfindex: ulHostIfindex,
	}

	// Load NIC ingress program
	if err := LoadNicIngressObjects(&m.nicObjs, nil); err != nil {
		return nil, fmt.Errorf("loading NIC ingress BPF objects: %w", err)
	}

	// Load ul-host ingress program
	if err := LoadUlHostIngressObjects(&m.ulObjs, nil); err != nil {
		_ = m.nicObjs.Close()
		return nil, fmt.Errorf("loading ul-host ingress BPF objects: %w", err)
	}

	// Write ul-host ifindex into NIC ingress config_map (key 0)
	key := uint32(0)
	val := uint32(ulHostIfindex)
	if err := m.nicObjs.ConfigMap.Put(key, val); err != nil {
		_ = m.nicObjs.Close()
		_ = m.ulObjs.Close()
		return nil, fmt.Errorf("writing ul-host ifindex to NIC config_map: %w", err)
	}

	// Write NIC ifindex into ul-host ingress config_map (key 0)
	val = uint32(nicIfindex)
	if err := m.ulObjs.ConfigMap.Put(key, val); err != nil {
		_ = m.nicObjs.Close()
		_ = m.ulObjs.Close()
		return nil, fmt.Errorf("writing NIC ifindex to ul-host config_map: %w", err)
	}

	// Try TCX first; fall back to legacy TC cls_bpf on older kernels.
	if err := m.attachTCX(nicIfindex, ulHostIfindex); err != nil {
		slog.Info("TCX attach failed, falling back to legacy TC cls_bpf", "error", err)
		if err := m.attachLegacyTC(nicIfindex, ulHostIfindex); err != nil {
			_ = m.nicObjs.Close()
			_ = m.ulObjs.Close()
			return nil, fmt.Errorf("attaching BPF programs: %w", err)
		}
	}

	return m, nil
}

// attachTCX attaches programs using TCX (kernel >= 6.6).
func (m *Manager) attachTCX(nicIfindex, ulHostIfindex int) error {
	nicTCXLink, err := link.AttachTCX(link.TCXOptions{
		Interface: nicIfindex,
		Program:   m.nicObjs.NicIngress,
		Attach:    ebpf.AttachTCXIngress,
	})
	if err != nil {
		return fmt.Errorf("attaching NIC ingress TCX: %w", err)
	}
	m.nicLink = nicTCXLink

	ulTCXLink, err := link.AttachTCX(link.TCXOptions{
		Interface: ulHostIfindex,
		Program:   m.ulObjs.UlHostIngress,
		Attach:    ebpf.AttachTCXIngress,
	})
	if err != nil {
		_ = m.nicLink.Close()
		m.nicLink = nil
		return fmt.Errorf("attaching ul-host ingress TCX: %w", err)
	}
	m.ulLink = ulTCXLink

	return nil
}

// attachLegacyTC attaches programs using legacy TC cls_bpf filters.
// This works on kernels as old as 4.1 (cls_bpf direct-action).
func (m *Manager) attachLegacyTC(nicIfindex, ulHostIfindex int) error {
	m.legacyTC = true

	nicProto, err := attachTCFilter(nicIfindex, m.nicObjs.NicIngress, "nic_ingress")
	if err != nil {
		return fmt.Errorf("attaching NIC ingress TC filter: %w", err)
	}
	m.nicFilterProto = nicProto

	ulProto, err := attachTCFilter(ulHostIfindex, m.ulObjs.UlHostIngress, "ul_host_ingress")
	if err != nil {
		_ = detachTCFilter(nicIfindex, m.nicFilterProto)
		return fmt.Errorf("attaching ul-host ingress TC filter: %w", err)
	}
	m.ulFilterProto = ulProto

	return nil
}

// attachTCFilter creates a clsact qdisc (if needed) and attaches a BPF
// program as a TC ingress filter with direct-action.
func attachTCFilter(ifindex int, prog *ebpf.Program, name string) (uint16, error) {
	link, err := netlink.LinkByIndex(ifindex)
	if err != nil {
		return 0, fmt.Errorf("finding link by ifindex %d: %w", ifindex, err)
	}

	// Ensure clsact qdisc exists
	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: ifindex,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscAdd(qdisc); err != nil {
		// Ignore "file exists" — qdisc may already be present
		if !errors.Is(err, unix.EEXIST) {
			return 0, fmt.Errorf("adding clsact qdisc to %s: %w", link.Attrs().Name, err)
		}
	}

	// Use a unique protocol number derived from the program name to
	// allow identification for cleanup. ETH_P_ALL would match everything
	// so we pick unused protocol numbers in the private range.
	proto := uint16(0x0800 + uint16(name[0]))

	fd := prog.FD()
	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: ifindex,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Handle:    0x1,
			Protocol:  proto,
			Priority:  1,
		},
		Fd:           fd,
		Name:         name,
		DirectAction: true,
	}
	if err := netlink.FilterAdd(filter); err != nil {
		return 0, fmt.Errorf("adding TC filter %s to %s: %w", name, link.Attrs().Name, err)
	}

	return proto, nil
}

// detachTCFilter removes a TC ingress filter by protocol number.
func detachTCFilter(ifindex int, proto uint16) error {
	filter := &netlink.BpfFilter{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: ifindex,
			Parent:    netlink.HANDLE_MIN_INGRESS,
			Handle:    0x1,
			Protocol:  proto,
			Priority:  1,
		},
	}
	return netlink.FilterDel(filter)
}

// UpdateNeighbors performs a diff-based update of the neighbor_map.
// It adds missing entries and removes stale ones without clearing the
// map, avoiding transient windows where traffic could be dropped.
func (m *Manager) UpdateNeighbors(ips []net.IP) error {
	desired := make(map[[4]byte]bool)
	for _, ip := range ips {
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		var k [4]byte
		copy(k[:], ip4)
		desired[k] = true
	}

	// Remove entries not in the desired set
	var key [4]byte
	var val uint8
	iter := m.nicObjs.NeighborMap.Iterate()
	for iter.Next(&key, &val) {
		if !desired[key] {
			_ = m.nicObjs.NeighborMap.Delete(key)
		}
	}

	// Add missing entries
	flag := uint8(1)
	for k := range desired {
		if err := m.nicObjs.NeighborMap.Put(k, flag); err != nil {
			return fmt.Errorf("adding neighbor to BPF map: %w", err)
		}
	}

	return nil
}

// UpdateVNIs performs a diff-based update of the vni_map.
// It adds missing entries and removes stale ones without clearing the
// map, avoiding transient windows where VXLAN traffic could be dropped.
func (m *Manager) UpdateVNIs(vnis []uint32) error {
	desired := make(map[uint32]bool, len(vnis))
	for _, vni := range vnis {
		desired[vni] = true
	}

	// Remove entries not in the desired set
	var key uint32
	var val uint8
	iter := m.nicObjs.VniMap.Iterate()
	for iter.Next(&key, &val) {
		if !desired[key] {
			_ = m.nicObjs.VniMap.Delete(key)
		}
	}

	// Add missing entries
	flag := uint8(1)
	for vni := range desired {
		if err := m.nicObjs.VniMap.Put(vni, flag); err != nil {
			return fmt.Errorf("adding VNI %d to BPF map: %w", vni, err)
		}
	}

	return nil
}

// Close detaches all eBPF programs and closes all resources.
func (m *Manager) Close() error {
	var errs []error

	if m.legacyTC {
		if err := detachTCFilter(m.nicIfindex, m.nicFilterProto); err != nil {
			errs = append(errs, fmt.Errorf("removing NIC TC filter: %w", err))
		}
		if err := detachTCFilter(m.ulHostIfindex, m.ulFilterProto); err != nil {
			errs = append(errs, fmt.Errorf("removing ul-host TC filter: %w", err))
		}
	} else {
		if m.nicLink != nil {
			if err := m.nicLink.Close(); err != nil {
				errs = append(errs, fmt.Errorf("closing NIC TCX link: %w", err))
			}
		}
		if m.ulLink != nil {
			if err := m.ulLink.Close(); err != nil {
				errs = append(errs, fmt.Errorf("closing ul-host TCX link: %w", err))
			}
		}
	}

	if err := m.nicObjs.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing NIC BPF objects: %w", err))
	}
	if err := m.ulObjs.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing ul-host BPF objects: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("closing BPF manager: %v", errs)
	}
	return nil
}
