// SPDX-License-Identifier: Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/openperouter/openperouter/internal/hostnetwork/bpf"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// sharedBPFManager is the singleton BPF manager for shared NIC mode.
// Only one underlay per node is supported, so a single manager suffices.
var sharedBPFManager *bpf.Manager

// Track the ifindexes the BPF manager was created with so we can
// detect stale attachments after a veth pair recreation.
var sharedBPFNicIfindex, sharedBPFUlHostIfindex int

// SetupSharedUnderlay configures the shared NIC mode underlay.
// It creates a veth pair (ul-host / ul-pe), copies the NIC's MAC and IPs
// to the namespace-side veth, loads and attaches eBPF TC programs, and
// populates the neighbor BPF map.
func SetupSharedUnderlay(ctx context.Context, params UnderlayParams) error {
	slog.InfoContext(ctx, "setting up shared underlay", "nic", params.UnderlayInterface, "neighbors", params.NeighborIPs)

	// 1. Create veth pair ul-host / ul-pe
	if err := setupNamespacedVeth(ctx, SharedUnderlayVethNames, params.TargetNS); err != nil {
		return fmt.Errorf("shared underlay: failed to create veth pair: %w", err)
	}

	// 2. Read NIC's MAC and IPs
	nic, err := netlink.LinkByName(params.UnderlayInterface)
	if err != nil {
		return fmt.Errorf("shared underlay: failed to find NIC %s: %w", params.UnderlayInterface, err)
	}
	nicMAC := nic.Attrs().HardwareAddr
	nicMTU := nic.Attrs().MTU

	nicAddrs, err := netlink.AddrList(nic, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("shared underlay: failed to list NIC %s addresses: %w", params.UnderlayInterface, err)
	}

	// 3. Set ul-host MTU
	ulHost, err := netlink.LinkByName(SharedUnderlayVethNames.HostSide)
	if err != nil {
		return fmt.Errorf("shared underlay: failed to find %s: %w", SharedUnderlayVethNames.HostSide, err)
	}
	if ulHost.Attrs().MTU != nicMTU {
		if err := netlink.LinkSetMTU(ulHost, nicMTU); err != nil {
			return fmt.Errorf("shared underlay: failed to set %s MTU to %d: %w", SharedUnderlayVethNames.HostSide, nicMTU, err)
		}
	}

	// 4. In router pod namespace: configure ul-pe
	ns, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("shared underlay: failed to get namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := ns.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := netnamespace.In(ns, func() error {
		ulPE, err := netlink.LinkByName(SharedUnderlayVethNames.NamespaceSide)
		if err != nil {
			return fmt.Errorf("failed to find %s in namespace: %w", SharedUnderlayVethNames.NamespaceSide, err)
		}

		// Set ul-pe MTU
		if ulPE.Attrs().MTU != nicMTU {
			if err := netlink.LinkSetMTU(ulPE, nicMTU); err != nil {
				return fmt.Errorf("failed to set %s MTU to %d: %w", SharedUnderlayVethNames.NamespaceSide, nicMTU, err)
			}
		}

		// Set ul-pe MAC = NIC MAC
		if err := netlink.LinkSetHardwareAddr(ulPE, nicMAC); err != nil {
			return fmt.Errorf("failed to set %s MAC to %s: %w", SharedUnderlayVethNames.NamespaceSide, nicMAC, err)
		}

		// Assign NIC's IPs to ul-pe
		for _, addr := range nicAddrs {
			if err := assignIPToInterface(ulPE, addr.IPNet.String()); err != nil {
				slog.DebugContext(ctx, "shared underlay: assign NIC IP to ul-pe", "addr", addr.IPNet.String(), "err", err)
			}
		}

		// Assign special marker IP so HasUnderlayInterface() works
		if err := assignIPToInterface(ulPE, underlayInterfaceSpecialAddr); err != nil {
			return fmt.Errorf("failed to assign marker IP to %s: %w", SharedUnderlayVethNames.NamespaceSide, err)
		}

		if err := netlink.LinkSetUp(ulPE); err != nil {
			return fmt.Errorf("failed to set %s up: %w", SharedUnderlayVethNames.NamespaceSide, err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("shared underlay: namespace configuration failed: %w", err)
	}

	// 5. Load and attach eBPF programs (idempotent).
	// Only re-create if the manager doesn't exist yet or the ifindexes
	// changed (e.g. veth pair was recreated after a router pod restart).
	nicIfindex := nic.Attrs().Index
	ulHostIfindex := ulHost.Attrs().Index

	if sharedBPFManager != nil && (sharedBPFNicIfindex != nicIfindex || sharedBPFUlHostIfindex != ulHostIfindex) {
		slog.InfoContext(ctx, "shared underlay: ifindexes changed, re-creating BPF manager",
			"old_nic", sharedBPFNicIfindex, "new_nic", nicIfindex,
			"old_ulhost", sharedBPFUlHostIfindex, "new_ulhost", ulHostIfindex)
		if err := sharedBPFManager.Close(); err != nil {
			slog.WarnContext(ctx, "shared underlay: failed to close old BPF manager", "error", err)
		}
		sharedBPFManager = nil
	}

	if sharedBPFManager == nil {
		mgr, err := bpf.NewManager(nicIfindex, ulHostIfindex)
		if err != nil {
			return fmt.Errorf("shared underlay: failed to create BPF manager: %w", err)
		}
		sharedBPFManager = mgr
		sharedBPFNicIfindex = nicIfindex
		sharedBPFUlHostIfindex = ulHostIfindex
	}

	// 6. Populate neighbor_map from params.NeighborIPs
	neighborIPs := make([]net.IP, 0, len(params.NeighborIPs))
	for _, ipStr := range params.NeighborIPs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			slog.WarnContext(ctx, "shared underlay: skipping invalid neighbor IP", "ip", ipStr)
			continue
		}
		neighborIPs = append(neighborIPs, ip)
	}
	if err := sharedBPFManager.UpdateNeighbors(neighborIPs); err != nil {
		return fmt.Errorf("shared underlay: failed to update neighbor map: %w", err)
	}

	// 7. If EVPN configured, create loopback with VTEP IP
	if params.EVPN != nil && params.EVPN.VtepIP != "" {
		if err := ensureLoopback(ctx, ns, params.EVPN.VtepIP); err != nil {
			return fmt.Errorf("shared underlay: failed to create loopback: %w", err)
		}
	}

	slog.InfoContext(ctx, "shared underlay setup complete")
	return nil
}

// UpdateSharedUnderlayVNIs updates the VNI BPF map with the current set of VNIs.
// This should be called after VNI configuration changes during reconciliation.
func UpdateSharedUnderlayVNIs(vnis []uint32) error {
	if sharedBPFManager == nil {
		return fmt.Errorf("shared underlay BPF manager not initialized")
	}
	return sharedBPFManager.UpdateVNIs(vnis)
}

// RemoveSharedUnderlay tears down the shared underlay: detaches BPF programs
// and removes the veth pair.
func RemoveSharedUnderlay() error {
	if sharedBPFManager != nil {
		if err := sharedBPFManager.Close(); err != nil {
			slog.Error("failed to close BPF manager", "error", err)
		}
		sharedBPFManager = nil
		sharedBPFNicIfindex = 0
		sharedBPFUlHostIfindex = 0
	}

	// Remove the veth pair (deleting one side removes both)
	ulHost, err := netlink.LinkByName(SharedUnderlayVethNames.HostSide)
	if err != nil {
		// Already gone
		return nil
	}
	if err := netlink.LinkDel(ulHost); err != nil {
		return fmt.Errorf("failed to delete %s: %w", SharedUnderlayVethNames.HostSide, err)
	}
	return nil
}
