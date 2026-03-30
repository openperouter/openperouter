// SPDX-License-Identifier:Apache-2.0

package main

// check_veths recreates veths if necessary. It monitors network link events in
// real-time by subscribing to netlink updates, reacting immediately to link
// changes.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	maxInterfaceLength = 15 // Linux restriction.
	tempSuffix         = "_t"
)

var (
	containerEngine = "docker"
)

func init() {
	if ce, overwrite := os.LookupEnv("CONTAINER_ENGINE_CLI"); overwrite {
		containerEngine = ce
	}
}

// vethPair represents a connected pair of veth interfaces.
type vethPair struct {
	left, right *Veth
}

// Veth represents one side of a veth pair with optional container/bridge attachment and IP addresses.
// A veth must be attached to either a container OR a bridge, but not both.
// Bridge-attached veths cannot have IP addresses assigned.
type Veth struct {
	Name      string   `json:"name"`
	Container string   `json:"container,omitempty"`
	Bridge    string   `json:"bridge,omitempty"`
	IPs       []string `json:"ips,omitempty"`
	netns     *netns.NsHandle
	isTemp    bool
}

// GetName returns the current interface name (which may be temp or permanent).
func (v *Veth) GetName() string {
	if v.isTemp {
		return v.GetTempName()
	}
	return v.Name
}

// GetTempName returns the temp name for the interface.
func (v *Veth) GetTempName() string {
	return fmt.Sprintf("%s%s", v.Name, tempSuffix)
}

// String returns the name of the veth interface.
func (v *Veth) String() string {
	return v.Name
}

// Init initializes the veth by setting up its network namespace handle.
// Must be called before doing anything with this veth.
func (v *Veth) Init(useTemp bool) {
	if v.netns != nil {
		_ = v.Close()
	}
	v.isTemp = useTemp
	ns := netns.None()
	v.netns = &ns
}

// Close closes the veth.
func (v *Veth) Close() error {
	if v.netns == nil {
		return nil
	}
	if err := v.netns.Close(); err != nil {
		return err
	}
	v.netns = nil
	return nil
}

// IsValid validates the veth configuration.
// Returns an error if both container and bridge are set, or if none is set, or if a bridge-attached veth has IP
// addresses.
func (v *Veth) IsValid() error {
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
	return nil
}

// Exists checks whether the veth interface exists in its target namespace with the final Name.
// Returns true if the interface exists, false otherwise, or an error if the check fails.
func (v *Veth) Exists() (bool, error) {
	ns, err := getNetNSFromContainer(v.Container)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = ns.Close()
	}()

	handle, err := netlink.NewHandleAt(*ns)
	if err != nil {
		return false, fmt.Errorf("could not switch to netns for veth %s, %w", v, err)
	}
	defer handle.Close()

	if _, err = handle.LinkByName(v.Name); err == nil {
		return true, nil
	}
	return false, nil
}

// AssignIPs adds the configured IP addresses to the veth interface.
// Operates within the veth's target namespace (container or host).
func (v *Veth) AssignIPs() error {
	handle, err := netlink.NewHandleAt(*v.netns)
	if err != nil {
		return fmt.Errorf("could not switch to netns for veth %s, %w", v, err)
	}
	defer handle.Close()

	intf, err := handle.LinkByName(v.GetName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.GetName(), err)
	}

	for _, ip := range v.IPs {
		log.Printf("\tAdding IP %v to %s", ip, v.GetName())
		addr, err := netlink.ParseAddr(ip)
		if err != nil {
			return fmt.Errorf("cannot parse IP %s for veth %s, %w", ip, v, err)
		}
		if err := handle.AddrAdd(intf, addr); err != nil {
			return fmt.Errorf("cannot add IP %s to veth %s, %w", addr, v.GetName(), err)
		}
	}
	return nil
}

// MoveToContainer moves the veth interface from the host namespace into the specified container's namespace.
// The container must be running and the namespace handle must be initialized via Init().
func (v *Veth) MoveToContainer() error {
	ns, err := getNetNSFromContainer(v.Container)
	if err != nil {
		return err
	}
	intf, err := netlink.LinkByName(v.GetName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.GetName(), err)
	}
	if err := netlink.LinkSetNsFd(intf, int(*ns)); err != nil {
		return fmt.Errorf("cannot move veth interface %s to container %s, %w", v.GetName(), v.Container, err)
	}
	if v.netns != nil {
		_ = v.netns.Close()
	}
	v.netns = ns
	return nil
}

// MoveToBridge attaches the veth interface to the specified bridge as a slave port.
// The bridge must already exist in the host namespace.
func (v *Veth) MoveToBridge() error {
	intf, err := netlink.LinkByName(v.GetName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.GetName(), err)
	}
	br, err := netlink.LinkByName(v.Bridge)
	if err != nil {
		return fmt.Errorf("cannot get bridge %s for veth %s, %w", v.Bridge, v, err)
	}
	if err := netlink.LinkSetMaster(intf, br); err != nil {
		return fmt.Errorf("cannot set bridge master %s for veth %s, %w", v.Bridge, v.GetName(), err)
	}
	return nil
}

// Up brings the veth interface up (sets it to the UP state).
// Operates within the veth's target namespace.
func (v *Veth) Up() error {
	handle, err := netlink.NewHandleAt(*v.netns)
	if err != nil {
		return fmt.Errorf("could not switch to netns for veth %s, %w", v.Name, err)
	}
	defer handle.Close()

	intf, err := handle.LinkByName(v.GetName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.GetName(), err)
	}
	if err := handle.LinkSetUp(intf); err != nil {
		return fmt.Errorf("cannot set veth %s up, %w", v.GetName(), err)
	}
	return nil
}

// MakePermanent moves the current veth from its temporary to its permanent name (if the interface is currently
// temporary).
func (v *Veth) MakePermanent() error {
	if !v.isTemp {
		return nil
	}
	handle, err := netlink.NewHandleAt(*v.netns)
	if err != nil {
		return fmt.Errorf("could not switch to netns for veth %s, %w", v.Name, err)
	}
	defer handle.Close()

	intf, err := handle.LinkByName(v.GetName())
	if err != nil {
		return fmt.Errorf("cannot get veth %s, %w", v.GetName(), err)
	}
	if err := handle.LinkSetName(intf, v.Name); err != nil {
		return fmt.Errorf("cannot set veth %s name to %s, %w", v.GetName(), v.Name, err)
	}
	v.isTemp = false
	return nil
}

// Cleanup will look for the interface both by temp and final name, in its namespace and in the host namespace, and will
// clean them up.
func (v *Veth) Cleanup() error {
	ns, err := getNetNSFromContainer(v.Container)
	if err != nil {
		return err
	}
	defer func() {
		_ = ns.Close()
	}()

	for _, n := range []netns.NsHandle{netns.None(), *ns} {
		handle, err := netlink.NewHandleAt(n)
		if err != nil {
			return fmt.Errorf("could not switch to netns for veth %s, %w", v.Name, err)
		}
		defer handle.Close()

		if intf, err := handle.LinkByName(v.Name); err == nil {
			_ = netlink.LinkDel(intf)
		}

		if intf, err := handle.LinkByName(v.GetTempName()); err == nil {
			_ = netlink.LinkDel(intf)
		}
	}
	return nil
}

// getNetNSFromContainer gets the namespace corresponding to container.
// If a container is specified, it retrieves the container's PID and opens its namespace.
// Otherwise, it uses the host's root namespace.
func getNetNSFromContainer(container string) (*netns.NsHandle, error) {
	if container == "" {
		ns := netns.None()
		return &ns, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, containerEngine, "inspect", "-f", "{{.State.Pid}}", container).Output()
	if err != nil {
		return nil, fmt.Errorf("cannot get PID for container %s, %w", container, err)
	}
	pid, err := strconv.Atoi(strings.Trim(string(out), "\n'"))
	if err != nil {
		return nil, fmt.Errorf("cannot parse PID for container %s, %w", container, err)
	}
	ns, err := netns.GetFromPid(pid)
	if err != nil {
		return nil, fmt.Errorf("cannot get netns for container %s, %w", container, err)
	}
	return &ns, nil
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

// ensureVeth creates a veth pair and configures both ends.
// It performs the following steps in order:
//  1. Cleans up any existing interfaces with the same names
//  2. Creates the veth pair in the host namespace
//  3. Moves interfaces to their target containers (if specified)
//  4. Attaches interfaces to bridges (if specified)
//  5. Assigns IP addresses to each interface
//  6. Brings both interfaces up
//
// Assumes left and right have been validated and initialized.
func ensureVeth(left, right *Veth) error {
	log.Print("\tCleanup if necessary")
	if err := left.Cleanup(); err != nil {
		return err
	}
	if err := right.Cleanup(); err != nil {
		return err
	}

	// Use workaround with temp interfaces only for containers to avoid some race conditions in tests.
	// See: https://github.com/openperouter/openperouter/commit/dd294c8192481e8ca1d4ac0d6ed79b6b8b5fc5d1
	// In short - the interface must be fully configured before giving it its final name, otherwise OpenPERouter could
	// move it into its namespace prematurely.
	// Interfaces for BGP unnumbered cannot be renamed, due to a bug with FRR-K8s BGP unnumbered. To be verified; so
	// add an exception for interfaces without IP addresses, as well.
	leftTemp := left.Container != "" && len(left.IPs) > 0
	log.Printf("\tRequest creation process for left (with temporary interface: %t)", leftTemp)
	left.Init(leftTemp)
	defer func() {
		_ = left.Close()
	}()
	rightTemp := right.Container != "" && len(right.IPs) > 0
	log.Printf("\tRequest creation process for right (with temporary interface: %t)", rightTemp)
	right.Init(rightTemp)
	defer func() {
		_ = right.Close()
	}()

	log.Print("\tCreate the veth pair")
	var err error
	la := netlink.NewLinkAttrs()
	la.Name = left.GetName()
	if la.HardwareAddr, err = randomMacAddress(); err != nil {
		return fmt.Errorf("could not generate random MAC address for link %s, %w", left.GetName(), err)
	}
	vethPair := netlink.Veth{
		LinkAttrs: la,
		PeerName:  right.GetName(),
	}
	if vethPair.PeerHardwareAddr, err = randomMacAddress(); err != nil {
		return fmt.Errorf("could not generate random MAC address for link %s, %w", right.GetName(), err)
	}
	log.Printf("\tVethPair attributes: %+v", vethPair)
	if err := netlink.LinkAdd(&vethPair); err != nil {
		return fmt.Errorf("cannot create link %s, %w", left.GetName(), err)
	}

	log.Print("\tMove interfaces to container network namespaces (if necessary)")
	if left.Container != "" {
		if err := left.MoveToContainer(); err != nil {
			return err
		}
	}
	if right.Container != "" {
		if err := right.MoveToContainer(); err != nil {
			return err
		}
	}

	log.Print("\tMove interfaces to bridges (if necessary)")
	if left.Bridge != "" {
		if err := left.MoveToBridge(); err != nil {
			return err
		}
	}
	if right.Bridge != "" {
		if err := right.MoveToBridge(); err != nil {
			return err
		}
	}

	log.Print("\tAssign IPs to each side")
	if err := right.AssignIPs(); err != nil {
		return err
	}
	if err := left.AssignIPs(); err != nil {
		return err
	}

	log.Print("\tSet both interfaces up")
	if err := left.Up(); err != nil {
		return err
	}
	if err := right.Up(); err != nil {
		return err
	}

	log.Print("\tRename interfaces to permanent name (if necessary)")
	if err := left.MakePermanent(); err != nil {
		return err
	}
	if err := right.MakePermanent(); err != nil {
		return err
	}

	return nil
}

// parseParameter parses a JSON parameter into a Veth struct.
// Expected JSON format: {"name":"veth0","container":"my-container","ips":["192.168.1.1/24"]}
func parseParameter(param string) (*Veth, error) {
	veth := &Veth{}
	if err := json.Unmarshal([]byte(param), veth); err != nil {
		return nil, err
	}
	return veth, nil
}

// reconcile continuously monitors and ensures all veth pairs exist.
// In order to do so, it subscribes to link updates inside the host and each container namespace,
// checking if pairs exist upon each link update and creating them if missing.
// Stops when the context is cancelled.
// We always only check the left veth, as this is either on the bridge, or on the leafkind.
// The right side could have been moved into the OpenPERouter pods, already.
func reconcile(ctx context.Context, vethPairs []vethPair) error {
	// Order vethPairs by namespace.
	containerToPair := make(map[string][]vethPair)
	for _, pair := range vethPairs {
		containerToPair[pair.left.Container] = append(containerToPair[pair.left.Container], pair)
	}

	// Monitor links in container namespaces.
	for container, pairs := range containerToPair {
		if container == "" {
			log.Print("Spawning link monitor in host namespace")
		} else {
			log.Printf("Spawning link monitor in namespace of container %s", container)
		}
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
					name := linkUpdate.Link.Attrs().Name
					log.Printf("Detected link delete event for %q inside container %q", name, container)
					for _, pair := range pairs {
						if pair.left.Name == linkUpdate.Link.Attrs().Name {
							log.Printf("Found matching pair %s <-> %s", pair.left, pair.right)
							exists, err := pair.left.Exists()
							if err != nil {
								log.Printf("ERROR: when checking if left veth exists, %v", err)
								continue
							}
							if exists {
								log.Printf("Skipping pair %s <-> %s as it exists", pair.left, pair.right)
								continue
							}
							log.Printf("=== Making sure pair %s <-> %s exists ===", pair.left, pair.right)
							if err := ensureVeth(pair.left, pair.right); err != nil {
								log.Printf("ERROR: cannot ensure that veths exist, %v", err)
								continue
							}
						}
					}
				}
			}
		}(ctx, linkUpdatesCh, pairs, container)

		// Getting namespace and then subscribing link events in namespace and sending them to channel.
		ns, err := getNetNSFromContainer(container)
		if err != nil {
			return fmt.Errorf("cannot create subscription for container %q, err: %w", container, err)
		}
		if err := netlink.LinkSubscribeAt(*ns, linkUpdatesCh, ctx.Done()); err != nil {
			return fmt.Errorf("cannot create subscription for container %q, err: %w", container, err)
		}
	}
	<-ctx.Done()
	return nil
}

// printHelp displays usage information and examples.
func printHelp() {
	fmt.Println(`Usage: check_veths <left_veth_json> <right_veth_json> [<left_veth_json> <right_veth_json> ...]

Create and monitor veth pairs, attach them to containers or bridges and assign IP addresses whenever they are deleted.

IMPORTANT: For each veth pair, the first veth is the interface that is monitored and it must be attached to either:
  - bridge "leafkind-switch", OR
  - container "clab-kind-leafkind"
IMPORTANT: All links that shall be monitored must exist prior to starting this script.

Parameter Format:
  Each veth is specified as a JSON object with the following fields:
    - "name"      (string, required): interface name
    - "container" (string, required*): container name to attach to
    - "bridge"    (string, required*): bridge name to attach to
    - "ips"       (array, optional):  IP addresses to assign (e.g., ["192.168.1.1/24", "2001:db8::1/64"])

  * Either "container" OR "bridge" must be set (but not both).
    A veth attached to a bridge cannot have IP addresses assigned.

Examples:
  # Veth pair: bridge-attached to container-attached
  check_veths \
    '{"name":"veth0","bridge":"leafkind-switch"}' \
    '{"name":"veth1","container":"my-container","ips":["192.168.1.1/24"]}'

  # Veth pair: container-attached to container-attached
  check_veths \
    '{"name":"veth2","container":"clab-kind-leafkind"}' \
    '{"name":"veth3","container":"other-container"}'

Environment Variables:
  CONTAINER_ENGINE_CLI: Container engine to use (default: "docker")

Options:
  -h, --help: Show this help message`)
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		printHelp()
		os.Exit(0)
	}
	if len(os.Args[1:])%2 != 0 {
		log.Fatal("must provide even number of arguments")
	}

	vethPairs := []vethPair{}
	for i := 1; i < len(os.Args); i = i + 2 {
		leftVeth, err := parseParameter(os.Args[i])
		if err != nil {
			log.Fatalf("cannot parse left parameter, %v", err)
		}
		rightVeth, err := parseParameter(os.Args[i+1])
		if err != nil {
			log.Fatalf("cannot parse right parameter, %v", err)
		}
		if err := leftVeth.IsValid(); err != nil {
			log.Fatalf("left parameter is not valid, %v", err)
		}
		if err := rightVeth.IsValid(); err != nil {
			log.Fatalf("right parameter is not valid, %v", err)
		}
		vethPairs = append(vethPairs, vethPair{leftVeth, rightVeth})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := reconcile(ctx, vethPairs); err != nil {
		log.Fatal(err)
	}
}
