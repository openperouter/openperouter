// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/grout/devicestate"
	"github.com/openperouter/openperouter/internal/hostnetwork"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/openperouter/openperouter/internal/pci"
	"github.com/vishvananda/netns"
	"k8s.io/apimachinery/pkg/util/sets"
)

// MaxTrunkVFIndex bounds the VF index to two hexadecimal digits in trunk
// port and VLAN sub-interface names.
const MaxTrunkVFIndex = 0xff

// trunkPFChars leaves room within IFNAMSIZ for the two-digit hexadecimal
// VF index and the three-digit hexadecimal VLAN tag.
const trunkPFChars = 10

var errUnsupportedTrunk = errors.New("only SR-IOV L2Trunks are supported by the grout datapath")

// ConfiguredTrunks holds the successfully configured L2Trunks, keyed by name.
type ConfiguredTrunks map[string]hostnetwork.L2TrunkParams

// Port returns the grout port of the named trunk, and whether the trunk
// was configured.
func (c ConfiguredTrunks) Port(trunkName string) (string, bool) {
	trunk, ok := c[trunkName]
	if !ok {
		return "", false
	}
	return GroutPortNameForTrunk(trunk), true
}

// GroutPortNameForTrunk returns the last 10 characters of the trunk's PF
// name followed by its VF index in two hexadecimal digits. A trunk with no
// SR-IOV parameters has no port, so its name is empty.
func GroutPortNameForTrunk(params hostnetwork.L2TrunkParams) string {
	if params.SRIOV == nil {
		return ""
	}
	return fmt.Sprintf("%s%02x", pfNameSuffix(params.SRIOV.PFName, trunkPFChars), params.SRIOV.VFIndex)
}

// SetupL2Trunk binds the VF of an SR-IOV L2Trunk to grout as a dedicated DPDK
// port. The PF-side settings of the VF are left to the user. The original state of the VF
// is saved in store, keyed by the trunk's port name, before it is touched, so it can
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
	if params.SRIOV.VFIndex < 0 || params.SRIOV.VFIndex > MaxTrunkVFIndex {
		return fmt.Errorf("vfIndex %d must be between 0 and %d", params.SRIOV.VFIndex, MaxTrunkVFIndex)
	}
	portName := GroutPortNameForTrunk(params)
	state, err := ensureTrunkDeviceState(portName, params.SRIOV, store, sysfs)
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
		return configureTrunkPort(ctx, client, portName, trunkPortOptions(params.SRIOV), state)
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

// RemoveNonConfiguredL2Trunks removes the trunks that are not desired
// anymore: deletes the VLAN sub-interfaces on their port and the port itself, then
// hands their VFs back to the kernel as described by the saved device state.
// The state is deleted last, so an interrupted teardown is resumed by
// the next call.
func RemoveNonConfiguredL2Trunks(
	ctx context.Context,
	client *Client,
	targetNS string,
	configuredTrunks []hostnetwork.L2TrunkParams,
	store devicestate.Store,
	sysfs pci.Sysfs,
) error {
	configuredTrunkPortNames := sets.New[string]()
	for _, trunk := range configuredTrunks {
		configuredTrunkPortNames.Insert(GroutPortNameForTrunk(trunk))
	}

	trunkDeviceStates, err := store.List()
	if err != nil {
		return fmt.Errorf("failed to list saved trunk device states: %w", err)
	}

	removedPorts := sets.New[string]()
	for _, state := range trunkDeviceStates {
		if configuredTrunkPortNames.Has(state.DeviceID) {
			continue
		}

		slog.InfoContext(ctx, "removing stale trunk", "port", state.DeviceID, "pciAddress", state.PCIAddress)
		if err := teardownL2Trunk(ctx, client, targetNS, state.DeviceID, state, sysfs); err != nil {
			return err
		}

		if err := store.Delete(state.DeviceID); err != nil {
			return fmt.Errorf("failed to delete trunk device state for %s: %w", state.DeviceID, err)
		}
		removedPorts.Insert(state.DeviceID)
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

// ensureTrunkDeviceState returns the saved state of the trunk VF, retrieving
// it from the host and saving it when missing. The state is retrieved once,
// before the VF is touched, and never updated afterwards.
func ensureTrunkDeviceState(
	portName string,
	sriov *hostnetwork.SRIOVTrunkParams,
	store devicestate.Store,
	sysfs pci.Sysfs,
) (devicestate.State, error) {
	state, err := store.Load(portName)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, devicestate.ErrNotFound) {
		return devicestate.State{}, fmt.Errorf("failed to load trunk device state for %s: %w", portName, err)
	}

	state, err = retrieveTrunkDeviceState(sriov, sysfs)
	if err != nil {
		return devicestate.State{}, err
	}
	if err := store.Save(portName, state); err != nil {
		return devicestate.State{}, fmt.Errorf("failed to save trunk device state for %s: %w", portName, err)
	}
	return state, nil
}

// retrieveTrunkDeviceState retrieves the state of the trunk VF from its
// kernel netdev. The VF must still be bound to its kernel driver: one already
// bound to vfio-pci has no kernel netdev, and its original driver can no
// longer be recovered.
func retrieveTrunkDeviceState(
	sriov *hostnetwork.SRIOVTrunkParams,
	sysfs pci.Sysfs,
) (devicestate.State, error) {
	pciAddr, err := sysfs.PCIAddressForVF(sriov.PFName, int(sriov.VFIndex))
	if err != nil {
		return devicestate.State{}, err
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
		if err := quiesceBifurcatedNetdev(state.PCIAddress); err != nil {
			return err
		}
	}

	if err := client.setPortUp(ctx, portName); err != nil {
		return fmt.Errorf("failed to set grout trunk port %s up: %w", portName, err)
	}
	return nil
}

func deleteTrunkPort(ctx context.Context, client *Client, portName string) error {
	// to delete a trunk port, we need to delete its VLAN sub-interfaces first,
	// and then the port itself.
	if err := deleteVLanInterfaces(ctx, client, portName); err != nil {
		return err
	}
	if err := client.deletePort(ctx, portName); err != nil {
		return fmt.Errorf("failed to delete grout trunk port %s: %w", portName, err)
	}
	return nil
}

// deleteVLanInterfaces deletes the VLAN sub-interfaces of a port. Grout
// refuses to delete a port that has sub-interfaces.
func deleteVLanInterfaces(ctx context.Context, client *Client, portName string) error {
	ifaces, err := client.listInterfaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if iface.Type != interfaceTypeVLAN {
			continue
		}

		info, err := client.getInterfaceInfo(ctx, iface.Name)
		if err != nil {
			return fmt.Errorf("failed to get interface %s: %w", iface.Name, err)
		}
		if info == nil || info.VLANParent != portName {
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

func trunkVLANPortName(portName string, tag int32) string {
	return fmt.Sprintf("%s%03x", portName, tag)
}

// pfNameSuffix returns the last limit characters of a PF name.
func pfNameSuffix(pfName string, limit int) string {
	if len(pfName) > limit {
		return pfName[len(pfName)-limit:]
	}
	return pfName
}
