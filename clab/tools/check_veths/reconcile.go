// SPDX-License-Identifier:Apache-2.0

package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

var (
	// containerEngine specifies the container runtime CLI to use.
	containerEngine = "docker"

	// validLeftContainers lists the containers that are allowed for left veth monitoring.
	validLeftContainers = []string{
		"clab-kind-leafkind",
		"clab-kind-leafkind-a",
		"clab-kind-leafkind-b",
	}
)

func init() {
	if ce, overwrite := os.LookupEnv("CONTAINER_ENGINE_CLI"); overwrite {
		containerEngine = ce
	}
}

// verifyVethPair validates the veth configuration for each veth.
// Returns an error if the left interface is not in the global namespace or in one of the required containers, and
// fails if for a veth both container and bridge are set, or if none is set, or if a bridge-attached veth has IP
// addresses.
func verifyVethPair(pair vethPair) error {
	if pair.Left.Container != "" && !slices.Contains(validLeftContainers, pair.Left.Container) {
		return fmt.Errorf("left veth must be either in the global namespace or in one of %+v", validLeftContainers)
	}
	for _, v := range []*veth{pair.Left, pair.Right} {
		maxLen := maxInterfaceLength - len(tempSuffix)
		if len(v.Name) > maxLen {
			return fmt.Errorf("veth %q is invalid. Length of name is %d, but maximum length is %d", v, len(v.Name), maxLen)
		}
		if v.Container != "" && v.Bridge != "" {
			return fmt.Errorf("veth %q is invalid, both container (%q) and bridge (%q) are set",
				v, v.Container, v.Bridge)
		}
		if v.Container == "" && v.Bridge == "" {
			return fmt.Errorf("veth %q is invalid, either container or bridge must be set",
				v)
		}
		if v.Bridge != "" && len(v.IPs) > 0 {
			return fmt.Errorf("veth %q is invalid, cannot have a bridge master (%q) and IP addresses (%+v)",
				v, v.Bridge, v.IPs)
		}
	}
	return nil
}

// prepareVethPair prepares the provided veth pair.
// Must be called before doing anything with the veths.
func prepareVethPair(pair vethPair) error {
	for _, v := range []*veth{pair.Left, pair.Right} {
		// Use workaround with temp interfaces only for containers to avoid some race conditions in tests.
		// See: https://github.com/openperouter/openperouter/commit/dd294c8192481e8ca1d4ac0d6ed79b6b8b5fc5d1
		// In short - the interface must be fully configured before giving it its final name, otherwise OpenPERouter could
		// move it into its namespace prematurely.
		// Interfaces for BGP unnumbered cannot be renamed, due to a bug with FRR-K8s BGP unnumbered. To be verified; so
		// add an exception for interfaces without IP addresses, as well.
		if v.Container != "" && len(v.IPs) > 0 {
			log.Printf("\tVeth %s needs temporary interface during creation", v)
			v.isTemp = true
		}

		v.containerPID = 1
		if v.Container != "" {
			pid, err := getDockerPID(v.Container)
			if err != nil {
				return err
			}
			v.containerPID = pid
		}
	}
	return nil
}

// reconcile continuously monitors and ensures all veth pairs exist.
// In order to do so, it subscribes to link updates inside the container namespace (or, if container == "", inside the
// host namespace) checking if pairs exist upon each link update and creating them if missing.
// Stops when the context is cancelled.
// We always only check the left veth for existence, as this is either on the bridge, or on the leafkind.
// The right side could have been moved into the OpenPERouter pods, already.
func reconcile(ctx context.Context, container string, pairs []vethPair) error {
	linkUpdatesCh := make(chan netlink.LinkUpdate)

	// Reconciler function consuming link updates from channel.
	go func(ctx context.Context, ch chan netlink.LinkUpdate, pairs []vethPair, container string) {
		for {
			select {
			case <-ctx.Done():
				return
			case linkUpdate, ok := <-ch:
				if !ok {
					return
				}
				if linkUpdate.Header.Type != syscall.RTM_DELLINK {
					continue
				}
				updatedLink := linkUpdate.Link.Attrs().Name
				log.Printf("Detected link delete event for %q inside container %q", updatedLink, container)
				for _, pair := range pairs {
					if pair.Left.Name != updatedLink {
						continue
					}
					log.Printf("Found matching monitored pair %s <-> %s", pair.Left, pair.Right)
					exists, err := pair.Left.exists()
					if err != nil {
						log.Printf("ERROR: when checking if left veth %s exists, %v", pair.Left, err)
						continue
					}
					if exists {
						log.Printf("Skipping pair %s <-> %s as it exists", pair.Left, pair.Right)
						continue
					}
					log.Printf("=== Making sure pair %s <-> %s exists ===", pair.Left, pair.Right)
					if err := ensureVeth(pair.Left, pair.Right); err != nil {
						log.Printf("ERROR: cannot ensure that veths exist, %v", err)
						continue
					}
				}
			}
		}
	}(ctx, linkUpdatesCh, pairs, container)

	// Getting namespace and then subscribing link events in namespace and sending them to channel.
	pid := 1
	if container != "" {
		var err error
		if pid, err = getDockerPID(container); err != nil {
			return fmt.Errorf("cannot get docker PID for subscription for container %q, err: %w", container, err)
		}
	}
	ns, err := netns.GetFromPid(pid)
	if err != nil {
		return fmt.Errorf("cannot get ns for subscription for container %q, err: %w", container, err)
	}
	if err := netlink.LinkSubscribeAt(ns, linkUpdatesCh, ctx.Done()); err != nil {
		return fmt.Errorf("cannot create subscription for container %q, err: %w", container, err)
	}

	return nil
}

// ensureVeth creates a veth pair and configures both ends.
// It performs the following steps in order:
//  1. Cleans up any existing interfaces with the same names
//  2. Creates the veth pair in the host namespace
//  3. Moves interfaces to their target containers (if specified)
//  4. Attaches interfaces to bridges (if specified)
//  5. Assigns IP addresses to each interface
//  6. Brings both interfaces up
//  7. Renames from temp name to final name (if required)
//
// Assumes left and right have been validated and initialized.
func ensureVeth(left, right *veth) error {
	log.Print("\tCleanup if necessary")
	if err := left.cleanup(); err != nil {
		return err
	}
	if err := right.cleanup(); err != nil {
		return err
	}

	log.Print("\tCreate the veth pair")
	var err error
	la := netlink.NewLinkAttrs()
	la.Name = left.getName()
	if la.HardwareAddr, err = randomMacAddress(); err != nil {
		return fmt.Errorf("could not generate random MAC address for link %s, %w", left.getName(), err)
	}
	vethPair := netlink.Veth{
		LinkAttrs: la,
		PeerName:  right.getName(),
	}
	if vethPair.PeerHardwareAddr, err = randomMacAddress(); err != nil {
		return fmt.Errorf("could not generate random MAC address for link %s, %w", right.getName(), err)
	}
	log.Printf("\tVethPair attributes: %+v", vethPair)
	if err := netlink.LinkAdd(&vethPair); err != nil {
		return fmt.Errorf("cannot create link %s, %w", left.getName(), err)
	}

	if left.Container != "" {
		log.Print("\tMove left interface to container network namespace")
		if err := left.moveToContainer(); err != nil {
			return err
		}
	}
	if right.Container != "" {
		log.Print("\tMove right interface to container network namespace")
		if err := right.moveToContainer(); err != nil {
			return err
		}
	}

	if left.Bridge != "" {
		log.Print("\tMove left interface to bridge")
		if err := left.enslaveToBridge(); err != nil {
			return err
		}
	}
	if right.Bridge != "" {
		log.Print("\tMove right interface to bridge")
		if err := right.enslaveToBridge(); err != nil {
			return err
		}
	}

	log.Print("\tAssign IPs to each side")
	if err := right.assignIPs(); err != nil {
		return err
	}
	if err := left.assignIPs(); err != nil {
		return err
	}

	log.Print("\tSet both interfaces up")
	if err := left.up(); err != nil {
		return err
	}
	if err := right.up(); err != nil {
		return err
	}

	if left.isTemp {
		log.Print("\tRename left interface to permanent name")
		if err := left.consolidate(); err != nil {
			return err
		}
	}
	if right.isTemp {
		log.Print("\tRename right interface to permanent name")
		if err := right.consolidate(); err != nil {
			return err
		}
	}

	return nil
}

// getDockerPID gets the PID of the main process inside the docker container.
// If the container name is empty, it returns 1 (the PID of the init process).
func getDockerPID(container string) (int, error) {
	if container == "" {
		return 0, fmt.Errorf("cannot get docker PID for empty container name")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, containerEngine, "inspect", "-f", "{{.State.Pid}}", container).Output()
	if err != nil {
		return -1, fmt.Errorf("cannot get ID for container %s, %w", container, err)
	}
	pid, err := strconv.Atoi(strings.Trim(string(out), "\n"))
	if err != nil {
		return -1, fmt.Errorf("cannot convert output to pid %s, output: %s, err: %w", container, out, err)
	}
	return pid, nil
}

// randomMacAddress generates a random MAC address, with the local bit set and the multicast bit
// unset.
// While not necessary on all systems, it's mandatory to explicitly generate and set a random MAC in github CI which
// will otherwise assign the same MAC address to interfaces.
func randomMacAddress() (net.HardwareAddr, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}

	// Set the local bit
	buf[0] |= 2
	// Clear multicast bit
	buf[0] &= 0xfe

	return net.HardwareAddr(buf), nil
}
