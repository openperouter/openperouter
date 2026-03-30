// SPDX-License-Identifier:Apache-2.0

package main

import (
	"fmt"
	"log"
	"net"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	maxInterfaceLength = 15   // Linux restriction.
	tempSuffix         = "_t" // Suffix appended to temporary interface names.
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
type veth struct {
	Name         string           // Interface name.
	Container    string           // Container to attach to (mutually exclusive with Bridge).
	Bridge       string           // Bridge to attach to (mutually exclusive with Container).
	IPs          []string         // IP addresses to assign to the interface.
	containerPID int              // PID of the container process (1 for host namespace).
	isTemp       bool             // Whether the interface is currently using a temporary name.
	macAddress   net.HardwareAddr // Store the MAC address of the veth (for reuse across recreations).
}

// String returns the name of the veth interface.
func (v *veth) String() string {
	if v.Container != "" {
		return fmt.Sprintf("%s/%s", v.Container, v.Name)
	}
	return v.Name
}

// getName returns the current interface name (which may be temp or permanent).
func (v *veth) getName() string {
	if v.isTemp {
		return v.getTempName()
	}
	return v.Name
}

// getTempName returns the temp name for the interface.
func (v *veth) getTempName() string {
	return fmt.Sprintf("%s%s", v.Name, tempSuffix)
}

// exists checks whether the veth interface exists in its target namespace with the final Name.
// Returns true if the interface exists, false otherwise, or an error if the check fails.
func (v *veth) exists() (bool, error) {
	handle, err := netlinkHandle(v.containerPID)
	if err != nil {
		return false, err
	}
	defer handle.Close()

	if _, err = handle.LinkByName(v.Name); err == nil {
		return true, nil
	}
	return false, nil
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
	if v.Container != "" {
		log.Print("\t\tMove interface to container network namespace")
		if err := v.moveToContainer(); err != nil {
			return err
		}
	}

	if v.Bridge != "" {
		log.Print("\t\tMove interface to bridge")
		if err := v.enslaveToBridge(); err != nil {
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

	if v.isTemp {
		log.Print("\t\tRename interface to permanent name")
		if err := v.consolidate(); err != nil {
			return err
		}
	}
	return nil
}

// assignIPs adds the configured IP addresses to the veth interface.
// Operates within the veth's target namespace (container or host).
// The veth must have been initialized and the container must be running.
func (v *veth) assignIPs() error {
	handle, err := netlinkHandle(v.containerPID)
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

// moveToContainer moves the veth interface from the host namespace into the specified container's namespace.
// The container must be running and the namespace handle must be initialized via Init().
func (v *veth) moveToContainer() error {
	intf, err := netlink.LinkByName(v.getName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getName(), err)
	}
	if err := netlink.LinkSetNsPid(intf, v.containerPID); err != nil {
		return fmt.Errorf("cannot move veth interface %s to container %s, %w", v.getName(), v.Container, err)
	}
	return nil
}

// enslaveToBridge attaches the veth interface to the specified bridge as a slave port.
// The bridge must already exist in the host namespace.
func (v *veth) enslaveToBridge() error {
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

// up brings the veth interface up (sets it to the UP state).
// Operates within the veth's target namespace.
func (v *veth) up() error {
	handle, err := netlinkHandle(v.containerPID)
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
// temporary).
func (v *veth) consolidate() error {
	handle, err := netlinkHandle(v.containerPID)
	if err != nil {
		return fmt.Errorf("could not switch to netns for veth %s, %w", v.Name, err)
	}
	defer handle.Close()

	intf, err := handle.LinkByName(v.getName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.getName(), err)
	}
	if err := handle.LinkSetName(intf, v.Name); err != nil {
		return fmt.Errorf("cannot set veth %s name to %s, %w", v.getName(), v.Name, err)
	}
	v.isTemp = false
	return nil
}

// reset deletes left over interfaces in case something went wrong on a previous attempt. In order to do so, it looks
// for the interface in both the global and container namespace, as well as by both temp and final name, and will clean
// them up.
func (v *veth) reset() error {
	containerHandle, err := netlinkHandle(v.containerPID)
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

		if intf, err := handle.LinkByName(v.getTempName()); err == nil {
			_ = handle.LinkDel(intf)
		}
	}
	return nil
}

// netlinkHandle returns the *netlink.Handle for the given PID.
func netlinkHandle(pid int) (*netlink.Handle, error) {
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
