// SPDX-License-Identifier:Apache-2.0

package main

import (
	"crypto/rand"
	"fmt"
	"log"
	"net"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	maxInterfaceLength = 15   // Linux restriction.
	tempSuffix         = "_t" // Suffix appended to temporary interface names.
	defaultMTU         = 9500 // Default MTU matches the default MTU of containerlab.
)

// vethPair represents a connected pair of veth interfaces.
type vethPair struct {
	Left  *veth
	Right *veth
}

// init cleans up and creates the vethPair on the Linux host.
func (v *vethPair) init() error {
	if err := v.Left.reset(); err != nil {
		return err
	}
	if err := v.Right.reset(); err != nil {
		return err
	}

	la := netlink.NewLinkAttrs()
	la.Name = v.Left.getName()
	la.HardwareAddr = v.Left.resolveMAC()
	ifVeth := netlink.Veth{
		LinkAttrs: la,
		PeerName:  v.Right.getName(),
	}
	ifVeth.PeerHardwareAddr = v.Right.resolveMAC()

	log.Printf("\tVethPair attributes: %+v", ifVeth)
	if err := netlink.LinkAdd(&ifVeth); err != nil {
		return fmt.Errorf("cannot create link %s, %w", v.Left.getName(), err)
	}

	return nil
}

// veth represents one side of a veth pair with optional container/bridge attachment and IP addresses.
// A veth must be attached to either a container OR a bridge, but not both.
// Bridge-attached veths cannot have IP addresses assigned.
// We use RequiresTemp only for the side of the veth pair that will be moved into kind-control-plane / kind-worker.
// The interface must be fully configured before being given its final name, otherwise the OpenPERouter controller pod
// could steal and move it into its namespace prematurely.
// See: https://github.com/openperouter/openperouter/commit/dd294c8192481e8ca1d4ac0d6ed79b6b8b5fc5d1
// The side of the veth pair that is plugged into leafkind must not be renamed, however, because FRR observes the
// rename and there is a bug in FRR-K8s BGP unnumbered that does not tolerate interface renames
// (see https://github.com/FRRouting/frr/issues/22022).
// The side of the veth pair that is connected into the switch does not need a temp name, either.
type veth struct {
	Name          string           // Interface name.
	Container     string           // Container to attach to (mutually exclusive with Bridge).
	Bridge        string           // Bridge to attach to (mutually exclusive with Container).
	IPs           []string         // IP addresses to assign to the interface (only for Containers).
	MTU           int              // MTU of the interface
	RequiresTemp  bool             `yaml:"requiresTemp"` // RequiresTemp requests temp interface during create operation.
	associatedPID int              // Host's init PID (1) or PID of the container process. Used to find correct netns.
	macAddress    net.HardwareAddr // Store the MAC address of the veth (for reuse across recreations).
}

// String returns the name of the veth interface.
func (v *veth) String() string {
	if v.Container != "" {
		return fmt.Sprintf("%s/%s", v.Container, v.Name)
	}
	return v.Name
}

// getName returns the interface name. It's either the temporary name if RequiresTemp is true, or the permanent name.
func (v *veth) getName() string {
	if v.RequiresTemp {
		return v.getTempName()
	}
	return v.Name
}

// getMTU returns the MTU of the interface. Defaults to 9500.
func (v *veth) getMTU() int {
	if v.MTU == 0 {
		return defaultMTU
	}
	return v.MTU
}

// getTempName returns the temp name for the interface.
func (v *veth) getTempName() string {
	return fmt.Sprintf("%s%s", v.Name, tempSuffix)
}

// exists checks whether the veth interface exists in its target namespace with the final Name.
// Returns true if the interface exists, false otherwise, or an error if the check fails.
func (v *veth) exists() (bool, error) {
	handle, err := netlinkHandleForPID(v.associatedPID)
	if err != nil {
		return false, err
	}
	defer handle.Close()

	if _, err = handle.LinkByName(v.Name); err == nil {
		return true, nil
	}
	return false, nil
}

// discoverAssociatedPID discovers the PID. For the host, this is always the
// PID of the init process (1), for containers, this is .State.Pid.
func (v *veth) discoverAssociatedPID() error {
	// In the host process, we are guaranteed to have PID 1 which is the init process.
	// For containers, discover the associated PID from .State.Pid.
	v.associatedPID = 1
	if v.Container != "" {
		pid, err := getContainerPID(v.Container)
		if err != nil {
			return err
		}
		v.associatedPID = pid
	}
	return nil
}

// discoverMAC stores the MAC address of the associated interface inside
// v.macAddress.
func (v *veth) discoverMAC() error {
	handle, err := netlinkHandleForPID(v.associatedPID)
	if err != nil {
		return err
	}
	defer handle.Close()

	link, err := handle.LinkByName(v.Name)
	if err != nil {
		return err
	}
	v.macAddress = link.Attrs().HardwareAddr
	return nil
}

// resolveMAC returns the MAC address for the interface.
// If no MAC address is known, it will generate and assign one, and return that address.
func (v *veth) resolveMAC() net.HardwareAddr {
	if len(v.macAddress) == 0 {
		v.macAddress = randomMacAddress()
	}
	return v.macAddress
}

// applyConfig applies the configuration to an already created Linux veth interface.
// The interface must already have been created and the veth must store a reference
// to the netlink interface.
func (v *veth) applyConfig() error {
	log.Print("\t\tSet interface MTU")
	if err := v.setMTU(); err != nil {
		return err
	}

	if v.Container != "" {
		log.Print("\t\tMove interface to container network namespace")
		if err := v.moveToContainer(); err != nil {
			return err
		}
	}

	if v.Bridge != "" {
		log.Print("\t\tAttach interface to bridge")
		if err := v.attachToBridge(); err != nil {
			return err
		}
	}

	log.Print("\t\tAssign IPs")
	if err := v.assignIPs(); err != nil {
		return err
	}

	log.Print("\t\tSet interface up")
	if err := v.up(); err != nil {
		return err
	}

	if v.RequiresTemp {
		log.Print("\t\tRename interface to permanent name")
		if err := v.consolidate(); err != nil {
			return err
		}
	}
	return nil
}

// setMTU sets the MTU for this interface. Defaults to 9500.
func (v *veth) setMTU() error {
	intf, err := netlink.LinkByName(v.getName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getName(), err)
	}
	if err := netlink.LinkSetMTU(intf, v.getMTU()); err != nil {
		return fmt.Errorf("cannot set MTU %d for interface %s, %w", v.getMTU(), v.getName(), err)
	}
	return nil
}

// moveToContainer moves the veth interface from the host namespace into the specified container's namespace.
// The container must be running and the namespace handle must be initialized via Init().
func (v *veth) moveToContainer() error {
	intf, err := netlink.LinkByName(v.getName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getName(), err)
	}
	if err := netlink.LinkSetNsPid(intf, v.associatedPID); err != nil {
		return fmt.Errorf("cannot move veth interface %s to container %s, %w", v.getName(), v.Container, err)
	}
	return nil
}

// attachToBridge attaches the veth interface to the specified bridge as a slave port.
// The bridge must already exist in the host namespace.
func (v *veth) attachToBridge() error {
	intf, err := netlink.LinkByName(v.getName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getName(), err)
	}
	br, err := netlink.LinkByName(v.Bridge)
	if err != nil {
		return fmt.Errorf("cannot get bridge %s for veth %s, %w", v.Bridge, v, err)
	}
	if err := netlink.LinkSetMaster(intf, br); err != nil {
		return fmt.Errorf("cannot set bridge master %s for veth %s, %w", v.Bridge, v.getName(), err)
	}
	return nil
}

// assignIPs adds the configured IP addresses to the veth interface.
// Operates within the veth's target namespace (container or host).
// The veth must have been initialized and the container must be running.
func (v *veth) assignIPs() error {
	handle, err := netlinkHandleForPID(v.associatedPID)
	if err != nil {
		return err
	}
	defer handle.Close()

	intf, err := handle.LinkByName(v.getName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getName(), err)
	}

	for _, ip := range v.IPs {
		log.Printf("\t\tAdding IP %v to %s", ip, v.getName())
		addr, err := netlink.ParseAddr(ip)
		if err != nil {
			return fmt.Errorf("cannot parse IP %s for veth %s, %w", ip, v, err)
		}
		if err := handle.AddrAdd(intf, addr); err != nil {
			return fmt.Errorf("cannot add IP %s to veth %s, %w", addr, v.getName(), err)
		}
	}
	return nil
}

// up brings the veth interface up (sets it to the UP state).
// Operates within the veth's target namespace.
func (v *veth) up() error {
	handle, err := netlinkHandleForPID(v.associatedPID)
	if err != nil {
		return fmt.Errorf("could not switch to netns for veth %s, %w", v.Name, err)
	}
	defer handle.Close()

	intf, err := handle.LinkByName(v.getName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getName(), err)
	}
	if err := handle.LinkSetUp(intf); err != nil {
		return fmt.Errorf("cannot set veth %s up, %w", v.getName(), err)
	}
	return nil
}

// consolidate moves the current veth from its temporary to its permanent name (if the interface is currently
// temporary). Because it renames the interface, it must be called at the very end.
func (v *veth) consolidate() error {
	handle, err := netlinkHandleForPID(v.associatedPID)
	if err != nil {
		return fmt.Errorf("could not switch to netns for veth %s, %w", v.Name, err)
	}
	defer handle.Close()

	intf, err := handle.LinkByName(v.getTempName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getTempName(), err)
	}
	if err := handle.LinkSetName(intf, v.Name); err != nil {
		return fmt.Errorf("cannot set veth %s name to %s, %w", v.getTempName(), v.Name, err)
	}
	return nil
}

// reset deletes left over interfaces in case something went wrong on a previous attempt. In order to do so, it looks
// for the interface in both the global and container namespace, as well as by both temp and final name, and will clean
// them up.
func (v *veth) reset() error {
	containerHandle, err := netlinkHandleForPID(v.associatedPID)
	if err != nil {
		return err
	}
	defer containerHandle.Close()

	globalHandle, err := netlink.NewHandleAt(netns.None())
	if err != nil {
		return err
	}
	defer globalHandle.Close()

	for _, handle := range []*netlink.Handle{globalHandle, containerHandle} {
		if intf, err := handle.LinkByName(v.Name); err == nil {
			_ = handle.LinkDel(intf)
		}

		if v.RequiresTemp {
			if intf, err := handle.LinkByName(v.getTempName()); err == nil {
				_ = handle.LinkDel(intf)
			}
		}
	}
	return nil
}

// netlinkHandleForPID returns the *netlink.Handle for the given PID.
func netlinkHandleForPID(pid int) (*netlink.Handle, error) {
	ns, err := netns.GetFromPid(pid)
	if err != nil {
		return nil, fmt.Errorf("could not open netns for PID %d, %w", pid, err)
	}
	defer func() {
		err := ns.Close()
		if err != nil {
			panic(err)
		}
	}()
	handle, err := netlink.NewHandleAt(ns)
	if err != nil {
		return nil, fmt.Errorf("could not switch to netns for PID %d, %w", pid, err)
	}
	return handle, err
}

// randomMacAddress generates a random MAC address, with the local bit set and the multicast bit
// unset.
// While not necessary on all systems, it's mandatory to explicitly generate and set a random MAC in github CI which
// will otherwise assign the same MAC address to interfaces.
func randomMacAddress() net.HardwareAddr {
	buf := make([]byte, 6)
	// Read never returns an error, so ignore.
	_, _ = rand.Read(buf)

	// Set the local bit
	buf[0] |= 2
	// Clear multicast bit
	buf[0] &= 0xfe

	return net.HardwareAddr(buf)
}
