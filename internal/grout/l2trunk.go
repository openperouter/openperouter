// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/vishvananda/netns"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// TrunkPortNamePrefix prefixes the grout ports dedicated to L2Trunks.
	TrunkPortNamePrefix = "t_"
)

var (
	errUnsupportedTrunk = errors.New("only SR-IOV L2Trunks are supported by the grout datapath")
	errTrunkVFInUse     = errors.New("the trunk VF is used by the grout port")
)

// L2TrunkPortName returns the name of the grout port of an SR-IOV L2Trunk, named
// after the PCI address of its VF.
func L2TrunkPortName(params hostnetwork.L2TrunkParams, sysfs pci.Sysfs) (string, error) {
	if params.SRIOV == nil {
		return "", errUnsupportedTrunk
	}
	pciAddr, err := sysfs.PCIAddressForVF(params.SRIOV.PFName, int(params.SRIOV.VFIndex))
	if err != nil {
		return "", err
	}
	return trunkPortName(pciAddr), nil
}

// SetupL2Trunk binds the VF of an SR-IOV L2Trunk to grout as a dedicated DPDK
// port. The PF-side settings of the VF are left to the user. The original state of the VF
// is saved in store, keyed by its PCI address, before it is touched, so it can
// be restored when the trunk is removed. Subsequent reconciles reuse the saved
// state even after the VF loses its kernel netdev.
func SetupL2Trunk(
	ctx context.Context,
	client *Client,
	params hostnetwork.L2TrunkParams,
	store devicestate.Store,
	sysfs pci.Sysfs,
) error {
	slog.DebugContext(ctx, "setup L2Trunk", "trunk", params.Name)
	defer slog.DebugContext(ctx, "setup L2Trunk done", "trunk", params.Name)

	if params.SRIOV == nil {
		return errUnsupportedTrunk
	}
	// The PF's virtfn symlink resolves the VF even when it has no kernel
	// netdev.
	pciAddr, err := sysfs.PCIAddressForVF(params.SRIOV.PFName, int(params.SRIOV.VFIndex))
	if err != nil {
		return err
	}
	if err := checkVFIsFree(ctx, client, pciAddr); err != nil {
		return err
	}

	state, err := ensureTrunkDeviceState(pciAddr, store, sysfs)
	if err != nil {
		return err
	}

	if err := removeSavedAddressesFromKernel(state); err != nil {
		return err
	}

	perouterNetNS, err := netns.GetFromPath(params.TargetNS)
	if err != nil {
		return fmt.Errorf("failed to find network namespace %s: %w", params.TargetNS, err)
	}
	defer func() {
		if err := perouterNetNS.Close(); err != nil {
			slog.Error("failed to close namespace", "namespace", params.TargetNS, "error", err)
		}
	}()

	if err := setupInterfaceForHWAcceleration(ctx, perouterNetNS, state.PCIAddress, state.InterfaceName,
		sysfs); err != nil {
		return fmt.Errorf("failed to prepare grout port driver for %s: %w", state.PCIAddress, err)
	}
	return netnamespace.In(perouterNetNS, func() error {
		return configureTrunkPort(ctx, client, trunkPortName(pciAddr), trunkPortOptions(params.SRIOV), state)
	})
}

// RemoveAllL2Trunks removes every trunk and hands the VFs back to the
// kernel.
func RemoveAllL2Trunks(
	ctx context.Context,
	client *Client,
	targetNS string,
	store devicestate.Store,
	sysfs pci.Sysfs,
) error {
	return RemoveNonConfiguredL2Trunks(ctx, client, targetNS, nil, store, sysfs)
}

// RemoveNonConfiguredL2Trunks removes the trunks whose port is not in
// configuredPorts: the VLAN sub-interfaces on their port and the port itself,
// and hands their VFs back to the kernel as described by the saved device
// state. The state is deleted last, so an interrupted teardown is resumed by
// the next call. Trunk ports without a saved state are removed too, but their
// VF cannot be restored.
func RemoveNonConfiguredL2Trunks(
	ctx context.Context,
	client *Client,
	targetNS string,
	configuredPorts []string,
	store devicestate.Store,
	sysfs pci.Sysfs,
) error {
	keptPorts := sets.New(configuredPorts...)

	states, err := store.List()
	if err != nil {
		return fmt.Errorf("failed to list saved trunk device states: %w", err)
	}

	removedPorts := sets.New[string]()
	for _, state := range states {
		portName := trunkPortName(state.PCIAddress)
		if keptPorts.Has(portName) {
			continue
		}
		slog.InfoContext(ctx, "removing stale trunk", "port", portName, "pciAddress", state.PCIAddress)
		if err := teardownL2Trunk(ctx, client, targetNS, portName, state, sysfs); err != nil {
			return err
		}
		if err := store.Delete(state.PCIAddress); err != nil {
			return fmt.Errorf("failed to delete trunk device state for %s: %w", state.PCIAddress, err)
		}
		removedPorts.Insert(portName)
	}

	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if !isTrunkPort(iface) || keptPorts.Has(iface.Name) || removedPorts.Has(iface.Name) {
			continue
		}
		slog.WarnContext(ctx, "removing trunk port without saved state, cannot restore its VF", "port", iface.Name)
		if err := deleteTrunkPort(ctx, client, iface.Name); err != nil {
			return err
		}
	}
	return nil
}

// teardownL2Trunk deletes the trunk port with its VLAN sub-interfaces, and
// hands the VF back to its original driver.
func teardownL2Trunk(
	ctx context.Context,
	client *Client,
	targetNS string,
	portName string,
	state devicestate.State,
	sysfs pci.Sysfs,
) error {
	if err := deleteTrunkPort(ctx, client, portName); err != nil {
		return err
	}
	return restoreAcceleratedDevice(ctx, targetNS, state, sysfs)
}

// checkVFIsFree fails when a grout port other than the trunk's own one, such
// as an underlay port, holds the trunk VF: DPDK cannot open the same PCI
// device twice, and the VF must not be touched while another port uses it.
func checkVFIsFree(ctx context.Context, client *Client, pciAddr string) error {
	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Type == interfaceTypePort && iface.Devargs == pciAddr && iface.Name != trunkPortName(pciAddr) {
			return fmt.Errorf("%w %s, VF %s", errTrunkVFInUse, iface.Name, pciAddr)
		}
	}
	return nil
}

// ensureTrunkDeviceState returns the saved state of the trunk VF, retrieving
// it from the host and saving it when missing. The state is retrieved once,
// before the VF is touched, and never updated afterwards.
func ensureTrunkDeviceState(pciAddr string, store devicestate.Store, sysfs pci.Sysfs) (devicestate.State, error) {
	state, err := store.Load(pciAddr)
	if errors.Is(err, devicestate.ErrNotFound) {
		return retrieveAndSaveTrunkDeviceState(pciAddr, store, sysfs)
	}
	if err != nil {
		return devicestate.State{}, fmt.Errorf("failed to load trunk device state for %s: %w", pciAddr, err)
	}
	return state, nil
}

func retrieveAndSaveTrunkDeviceState(
	pciAddr string,
	store devicestate.Store,
	sysfs pci.Sysfs,
) (devicestate.State, error) {
	state, err := retrieveTrunkDeviceState(pciAddr, sysfs)
	if err != nil {
		return devicestate.State{}, err
	}
	if err := store.Save(pciAddr, state); err != nil {
		return devicestate.State{}, fmt.Errorf("failed to save trunk device state for %s: %w", pciAddr, err)
	}
	return state, nil
}

// retrieveTrunkDeviceState retrieves the state of the trunk VF. A VF already
// bound to vfio-pci has no kernel netdev, and nothing to restore.
func retrieveTrunkDeviceState(pciAddr string, sysfs pci.Sysfs) (devicestate.State, error) {
	driver, err := sysfs.DriverForPCIAddress(pciAddr)
	if err != nil {
		return devicestate.State{}, fmt.Errorf("failed to read driver for %s: %w", pciAddr, err)
	}
	if driver == pci.DriverVFIOPCI {
		return devicestate.State{PCIAddress: pciAddr, OriginalDriver: driver}, nil
	}

	netdev, err := sysfs.NetDeviceForPCIAddress(pciAddr)
	if err != nil {
		return devicestate.State{}, err
	}
	return retrieveDeviceState(netdev, PortModeAccelerated, sysfs)
}

func configureTrunkPort(
	ctx context.Context,
	client *Client,
	portName string,
	opts portOptions,
	state devicestate.State,
) error {
	opts.Devargs = state.PCIAddress
	opts.MTU = state.MTU
	if err := client.ensurePort(ctx, portName, opts); err != nil {
		return fmt.Errorf("failed to create grout trunk port %s: %w", portName, err)
	}

	if pci.IsBifurcated(state.OriginalDriver) {
		if err := quiesceBifurcatedNetdev(state.InterfaceName); err != nil {
			return err
		}
	}

	if err := client.setPortUp(ctx, portName); err != nil {
		return fmt.Errorf("failed to set grout trunk port %s up: %w", portName, err)
	}
	return nil
}

func deleteTrunkPort(ctx context.Context, client *Client, portName string) error {
	if err := deleteSubinterfaces(ctx, client, portName); err != nil {
		return err
	}
	if err := client.deletePort(ctx, portName); err != nil {
		return fmt.Errorf("failed to delete grout trunk port %s: %w", portName, err)
	}
	return nil
}

// deleteSubinterfaces deletes the VLAN sub-interfaces of a port. Grout
// refuses to delete a port that has sub-interfaces, and only trunks create
// them.
func deleteSubinterfaces(ctx context.Context, client *Client, portName string) error {
	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Type != interfaceTypeVLAN || iface.VLANParent != portName {
			continue
		}
		if err := client.deleteInterface(ctx, iface.Name); err != nil {
			return fmt.Errorf("failed to delete VLAN %s of port %s: %w", iface.Name, portName, err)
		}
	}
	return nil
}

func trunkPortOptions(sriov *hostnetwork.SRIOVTrunkParams) portOptions {
	if sriov.AcceleratedConfig == nil {
		return portOptions{}
	}
	return portOptions{RXQueues: sriov.AcceleratedConfig.RXQueues, QSize: sriov.AcceleratedConfig.QSize}
}

// trunkPortName names a dedicated grout port after the PCI address of its
// VF, as a trunk name may not fit in a grout interface name. A PCI address
// (DDDD:BB:DD.F) has fixed-width fields, so dropping its separators keeps it
// unique and within IFNAMSIZ.
func trunkPortName(pciAddr string) string {
	return TrunkPortNamePrefix + strings.NewReplacer(":", "", ".", "").Replace(pciAddr)
}

func isTrunkPort(iface groutInterface) bool {
	return iface.Type == interfaceTypePort && strings.HasPrefix(iface.Name, TrunkPortNamePrefix)
}

// trunkVLANName names a VLAN sub-interface after its trunk port and tag, which
// identify it uniquely. The separator of the port prefix is dropped to fit a
// 4-digit tag within IFNAMSIZ.
func trunkVLANName(portName string, tag int32) string {
	return fmt.Sprintf("t%s.%d", strings.TrimPrefix(portName, TrunkPortNamePrefix), tag)
}

// deleteBridgeVLANs deletes the VLAN sub-interfaces attached to the bridge,
// except the one named keep.
func deleteBridgeVLANs(ctx context.Context, client *Client, bridgeName, keep string) error {
	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Type != interfaceTypeVLAN || iface.Domain != bridgeName || iface.Name == keep {
			continue
		}
		if err := client.deleteInterface(ctx, iface.Name); err != nil {
			return fmt.Errorf("failed to delete VLAN %s of bridge %s: %w", iface.Name, bridgeName, err)
		}
	}
	return nil
}
