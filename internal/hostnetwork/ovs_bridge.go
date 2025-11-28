// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/vishvananda/netlink"
)

const (
	defaultOVSSocketPath = "unix:/var/run/openvswitch/db.sock"
)

var OVSSocketPath = defaultOVSSocketPath

// Bridge model represents a row in the Bridge table
type Bridge struct {
	UUID        string            `ovsdb:"_uuid"`
	Name        string            `ovsdb:"name"`
	Ports       []string          `ovsdb:"ports"`
	ExternalIds map[string]string `ovsdb:"external_ids"`
}

// Port model represents a row in the Port table
type Port struct {
	UUID       string   `ovsdb:"_uuid"`
	Name       string   `ovsdb:"name"`
	Interfaces []string `ovsdb:"interfaces"`
}

// Interface model represents a row in the Interface table
type Interface struct {
	UUID string `ovsdb:"_uuid"`
	Name string `ovsdb:"name"`
	Type string `ovsdb:"type"`
}

// OpenVSwitch model represents a row in the Open_vSwitch table
type OpenVSwitch struct {
	UUID    string   `ovsdb:"_uuid"`
	Bridges []string `ovsdb:"bridges"`
}

func ensureOVSBridgeAndAttach(ctx context.Context, bridgeName, ifaceName string) error {
	slog.Info("ensureOVSBridgeAndAttach", "bridge", bridgeName, "interface", ifaceName)

	// Verify the interface exists before trying to attach to OVS
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %s does not exist on host (required before attaching to OVS bridge): %w", ifaceName, err)
	}
	slog.Info("interface exists on host", "name", ifaceName, "index", link.Attrs().Index, "type", link.Type())

	ovs, err := NewOVSClient(ctx)
	if err != nil {
		return err
	}
	defer ovs.Close()

	return ensureOVSBridgeAndAttachWithClient(ctx, ovs, bridgeName, ifaceName)
}

// NewOVSClient creates a new OVS database client
func NewOVSClient(ctx context.Context) (libovsclient.Client, error) {
	dbModel, err := model.NewClientDBModel("Open_vSwitch", map[string]model.Model{
		"Open_vSwitch": &OpenVSwitch{},
		"Bridge":       &Bridge{},
		"Port":         &Port{},
		"Interface":    &Interface{},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build OVS DB model: %w", err)
	}

	ovs, err := libovsclient.NewOVSDBClient(
		dbModel,
		libovsclient.WithEndpoint(OVSSocketPath),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create OVSDB client: %w", err)
	}
	if err := ovs.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to OVSDB: %w", err)
	}
	return ovs, nil
}

// ensureOVSBridgeAndAttachWithClient ensures an OVS bridge exists and attaches ifaceName as a port.
// This version accepts a client parameter for testing.
func ensureOVSBridgeAndAttachWithClient(ctx context.Context, ovs libovsclient.Client, bridgeName, ifaceName string) error {
	// Cache for indexed operations
	_, err := ovs.Monitor(ctx,
		ovs.NewMonitor(
			libovsclient.WithTable(&OpenVSwitch{}),
			libovsclient.WithTable(&Bridge{}),
			libovsclient.WithTable(&Port{}),
			libovsclient.WithTable(&Interface{}),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to setup monitor: %w", err)
	}

	bridgeUUID, err := EnsureBridge(ctx, ovs, bridgeName)
	if err != nil {
		return fmt.Errorf("failed to ensure OVS bridge %q exists: %w", bridgeName, err)
	}

	// Create the bridge management port (internal port) so the bridge appears as a Linux interface
	if err := ensureInternalPortForBridge(ctx, ovs, bridgeUUID, bridgeName); err != nil {
		return fmt.Errorf("failed to create internal port for bridge %q: %w", bridgeName, err)
	}

	if err := ensurePortAttachedToBridge(ctx, ovs, bridgeUUID, ifaceName); err != nil {
		return fmt.Errorf("failed to attach veth %q to bridge %q: %w", ifaceName, bridgeName, err)
	}

	// Bring the OVS bridge interface UP so it can be used as a master for macvlan/ipvlan
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to get OVS bridge %q interface: %w", bridgeName, err)
	}
	if err := netlink.LinkSetUp(bridge); err != nil {
		return fmt.Errorf("failed to bring OVS bridge %q UP: %w", bridgeName, err)
	}
	slog.Debug("OVS bridge interface is UP", "name", bridgeName)

	return nil
}

// EnsureBridge ensures an OVS bridge exists, creating it if necessary
func EnsureBridge(ctx context.Context, ovs libovsclient.Client, bridgeName string) (string, error) {
	br := &Bridge{Name: bridgeName}
	err := ovs.Get(ctx, br)
	if err == nil {
		return br.UUID, nil
	}
	if !errors.Is(err, libovsclient.ErrNotFound) {
		return "", fmt.Errorf("failed to check if bridge %q exists: %w", bridgeName, err)
	}

	namedUUID := "new_bridge"
	br = &Bridge{
		UUID:        namedUUID,
		Name:        bridgeName,
		ExternalIds: map[string]string{"created-by": "openperouter"},
	}

	insertOp, err := ovs.Create(br)
	if err != nil {
		return "", fmt.Errorf("failed to create bridge insert operation: %w", err)
	}

	ovsRow := &OpenVSwitch{}
	mutateOp, err := ovs.WhereCache(func(*OpenVSwitch) bool { return true }).
		Mutate(ovsRow, model.Mutation{
			Field:   &ovsRow.Bridges,
			Mutator: ovsdb.MutateOperationInsert,
			Value:   []string{namedUUID},
		})
	if err != nil {
		return "", fmt.Errorf("failed to create mutate operation: %w", err)
	}

	operations := append(insertOp, mutateOp...)
	reply, err := ovs.Transact(ctx, operations...)
	if err != nil {
		return "", fmt.Errorf("transaction failed: %w", err)
	}

	_, err = ovsdb.CheckOperationResults(reply, operations)
	if err != nil {
		return "", fmt.Errorf("operation failed: %w", err)
	}

	realUUID := reply[0].UUID.GoUUID
	slog.Debug("created OVS bridge", "name", bridgeName, "UUID", realUUID)

	return realUUID, nil
}

func ensurePortAttachedToBridge(ctx context.Context, ovs libovsclient.Client, bridgeUUID, interfaceName string) error {
	iface := &Interface{Name: interfaceName}
	interfaceUUID := ""
	err := ovs.Get(ctx, iface)
	if err == nil {
		interfaceUUID = iface.UUID
		slog.Debug("interface already exists", "name", interfaceName, "UUID", interfaceUUID)
	} else if !errors.Is(err, libovsclient.ErrNotFound) {
		return fmt.Errorf("failed to check if interface %q exists: %w", interfaceName, err)
	}

	port := &Port{Name: interfaceName}
	portUUID := ""
	err = ovs.Get(ctx, port)
	if err == nil {
		portUUID = port.UUID
		slog.Debug("port already exists", "name", interfaceName, "UUID", portUUID)
	} else if !errors.Is(err, libovsclient.ErrNotFound) {
		return fmt.Errorf("failed to check if port %q exists: %w", interfaceName, err)
	}

	bridge := &Bridge{UUID: bridgeUUID}
	if err := ovs.Get(ctx, bridge); err != nil {
		return fmt.Errorf("failed to get bridge: %w", err)
	}

	if portUUID != "" {
		for _, existingPortUUID := range bridge.Ports {
			if existingPortUUID == portUUID {
				slog.Debug("port already attached to bridge", "port", interfaceName, "bridge", bridge.Name)
				return nil
			}
		}
	}

	var operations []ovsdb.Operation
	interfaceNamedUUID := "new_interface"
	portNamedUUID := "new_port"

	if interfaceUUID == "" {
		interfaceOp, err := ovs.Create(
			&Interface{
				UUID: interfaceNamedUUID,
				Name: interfaceName,
				Type: "system", // system type for regular interfaces
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create interface operation: %w", err)
		}
		operations = append(operations, interfaceOp...)
	} else {
		interfaceNamedUUID = interfaceUUID
	}

	if portUUID == "" {
		portOp, err := ovs.Create(
			&Port{
				UUID:       portNamedUUID,
				Name:       interfaceName,
				Interfaces: []string{interfaceNamedUUID},
			},
		)
		if err != nil {
			return fmt.Errorf("failed to create port operation: %w", err)
		}
		operations = append(operations, portOp...)
	} else {
		portNamedUUID = portUUID
	}

	mutateOp, err := ovs.Where(bridge).Mutate(bridge, model.Mutation{
		Field:   &bridge.Ports,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{portNamedUUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create bridge mutate operation: %w", err)
	}
	operations = append(operations, mutateOp...)

	slog.Info("executing OVS transaction to attach port to bridge",
		"interface", interfaceName,
		"bridge", bridge.Name,
		"operations_count", len(operations))

	reply, err := ovs.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf("OVS transaction failed when attaching interface %s to bridge %s: %w", interfaceName, bridge.Name, err)
	}

	_, err = ovsdb.CheckOperationResults(reply, operations)
	if err != nil {
		return fmt.Errorf("OVS operation failed when attaching interface %s to bridge %s (this usually means the interface doesn't exist on the system or OVS can't access it): %w", interfaceName, bridge.Name, err)
	}

	slog.Info("successfully added interface to bridge", "name", interfaceName, "bridge", bridge.Name)

	return nil
}

// ensureInternalPortForBridge creates an OVS internal port for the bridge.
// This creates the Linux network interface that can be used as a master for macvlan/ipvlan.
func ensureInternalPortForBridge(ctx context.Context, ovs libovsclient.Client, bridgeUUID, bridgeName string) error {
	// Check if internal port already exists
	iface := &Interface{Name: bridgeName}
	err := ovs.Get(ctx, iface)
	if err == nil {
		slog.Debug("internal port already exists for bridge", "name", bridgeName, "UUID", iface.UUID)
		return nil
	}
	if !errors.Is(err, libovsclient.ErrNotFound) {
		return fmt.Errorf("failed to check if internal interface %q exists: %w", bridgeName, err)
	}

	port := &Port{Name: bridgeName}
	err = ovs.Get(ctx, port)
	if err == nil {
		slog.Debug("port already exists for bridge", "name", bridgeName, "UUID", port.UUID)
		return nil
	}
	if !errors.Is(err, libovsclient.ErrNotFound) {
		return fmt.Errorf("failed to check if internal port %q exists: %w", bridgeName, err)
	}

	// Create internal interface and port for the bridge
	var operations []ovsdb.Operation
	interfaceNamedUUID := "new_internal_interface"
	portNamedUUID := "new_internal_port"

	// Create internal interface (this creates the Linux interface)
	interfaceOp, err := ovs.Create(
		&Interface{
			UUID: interfaceNamedUUID,
			Name: bridgeName,
			Type: "internal", // internal type creates a Linux interface
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create internal interface operation: %w", err)
	}
	operations = append(operations, interfaceOp...)

	// Create port for the internal interface
	portOp, err := ovs.Create(
		&Port{
			UUID:       portNamedUUID,
			Name:       bridgeName,
			Interfaces: []string{interfaceNamedUUID},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create internal port operation: %w", err)
	}
	operations = append(operations, portOp...)

	// Attach the internal port to the bridge
	bridge := &Bridge{UUID: bridgeUUID}
	if err := ovs.Get(ctx, bridge); err != nil {
		return fmt.Errorf("failed to get bridge: %w", err)
	}

	mutateOp, err := ovs.Where(bridge).Mutate(bridge, model.Mutation{
		Field:   &bridge.Ports,
		Mutator: ovsdb.MutateOperationInsert,
		Value:   []string{portNamedUUID},
	})
	if err != nil {
		return fmt.Errorf("failed to create bridge mutate operation: %w", err)
	}
	operations = append(operations, mutateOp...)

	slog.Info("creating OVS internal port for bridge",
		"bridge", bridgeName,
		"operations_count", len(operations))

	reply, err := ovs.Transact(ctx, operations...)
	if err != nil {
		return fmt.Errorf("OVS transaction failed when creating internal port for bridge %s: %w", bridgeName, err)
	}

	_, err = ovsdb.CheckOperationResults(reply, operations)
	if err != nil {
		return fmt.Errorf("OVS operation failed when creating internal port for bridge %s: %w", bridgeName, err)
	}

	slog.Info("successfully created internal port for bridge", "name", bridgeName)
	return nil
}
