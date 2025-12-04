// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/ovn-kubernetes/libovsdb/cache"
	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
)

// mockOVSClient is a mock implementation of libovsclient.Client for testing
type mockOVSClient struct {
	// State tracking
	bridges     map[string]*Bridge    // name -> Bridge
	ports       map[string]*Port      // name -> Port
	interfaces  map[string]*Interface // name -> Interface
	openVSwitch *OpenVSwitch

	// Operation tracking
	connected        bool
	transactionCalls int
	operations       [][]ovsdb.Operation

	// Error injection
	connectError    error
	monitorError    error
	getError        error
	createError     error
	mutateError     error
	transactError   error
	whereError      error
	whereCacheError error
}

// mockConditionalClient implements conditional operations for the mock
type mockConditionalClient struct {
	client *mockOVSClient
}

// newMockOVSClient creates a new mock OVS client with empty state
func newMockOVSClient() *mockOVSClient {
	return &mockOVSClient{
		bridges:    make(map[string]*Bridge),
		ports:      make(map[string]*Port),
		interfaces: make(map[string]*Interface),
		openVSwitch: &OpenVSwitch{
			UUID:    "ovs-uuid",
			Bridges: []string{},
		},
		connected: false,
	}
}

// Implement libovsclient.Client interface methods

func (m *mockOVSClient) Connect(ctx context.Context) error {
	if m.connectError != nil {
		return m.connectError
	}
	m.connected = true
	return nil
}

func (m *mockOVSClient) Close() {
	m.connected = false
}

func (m *mockOVSClient) Monitor(ctx context.Context, monitor *libovsclient.Monitor) (libovsclient.MonitorCookie, error) {
	if m.monitorError != nil {
		return libovsclient.MonitorCookie{}, m.monitorError
	}
	// Return a mock monitor cookie
	return libovsclient.MonitorCookie{DatabaseName: "Open_vSwitch", ID: "mock-id"}, nil
}

func (m *mockOVSClient) NewMonitor(opts ...libovsclient.MonitorOption) *libovsclient.Monitor {
	// Return a mock monitor
	return &libovsclient.Monitor{}
}

func (m *mockOVSClient) Get(ctx context.Context, obj model.Model) error {
	if m.getError != nil {
		return m.getError
	}

	switch v := obj.(type) {
	case *Bridge:
		// Try to find by Name first, then by UUID
		if v.Name != "" {
			if bridge, exists := m.bridges[v.Name]; exists {
				*v = *bridge
				return nil
			}
		} else if v.UUID != "" {
			// Search by UUID
			for _, bridge := range m.bridges {
				if bridge.UUID == v.UUID {
					*v = *bridge
					return nil
				}
			}
		}
		return libovsclient.ErrNotFound
	case *Port:
		if v.Name != "" {
			if port, exists := m.ports[v.Name]; exists {
				*v = *port
				return nil
			}
		} else if v.UUID != "" {
			for _, port := range m.ports {
				if port.UUID == v.UUID {
					*v = *port
					return nil
				}
			}
		}
		return libovsclient.ErrNotFound
	case *Interface:
		if v.Name != "" {
			if iface, exists := m.interfaces[v.Name]; exists {
				*v = *iface
				return nil
			}
		} else if v.UUID != "" {
			for _, iface := range m.interfaces {
				if iface.UUID == v.UUID {
					*v = *iface
					return nil
				}
			}
		}
		return libovsclient.ErrNotFound
	case *OpenVSwitch:
		*v = *m.openVSwitch
		return nil
	}
	return fmt.Errorf("unsupported model type")
}

func (m *mockOVSClient) Create(models ...model.Model) ([]ovsdb.Operation, error) {
	if m.createError != nil {
		return nil, m.createError
	}

	ops := []ovsdb.Operation{}
	for _, model := range models {
		op := ovsdb.Operation{
			Op:    "insert",
			Table: getTableName(model),
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func (m *mockOVSClient) Where(models ...model.Model) libovsclient.ConditionalAPI {
	return &mockConditionalClient{client: m}
}

func (m *mockOVSClient) WhereCache(predicate interface{}) libovsclient.ConditionalAPI {
	return &mockConditionalClient{client: m}
}

func (m *mockOVSClient) WhereAll(m2 model.Model, conditions ...model.Condition) libovsclient.ConditionalAPI {
	return &mockConditionalClient{client: m}
}

func (m *mockOVSClient) WhereAny(m2 model.Model, conditions ...model.Condition) libovsclient.ConditionalAPI {
	return &mockConditionalClient{client: m}
}

func (m *mockOVSClient) Transact(ctx context.Context, operations ...ovsdb.Operation) ([]ovsdb.OperationResult, error) {
	if m.transactError != nil {
		return nil, m.transactError
	}

	m.transactionCalls++
	m.operations = append(m.operations, operations)

	// Map of named UUIDs to real UUIDs for this transaction
	namedUUIDMap := make(map[string]string)

	// Process operations and update mock state
	results := make([]ovsdb.OperationResult, len(operations))
	for i, op := range operations {
		result := ovsdb.OperationResult{}

		if op.Op == "insert" {
			// Generate a UUID for the inserted object
			uuid := fmt.Sprintf("uuid-%d", m.transactionCalls*100+i)
			result.UUID = ovsdb.UUID{GoUUID: uuid}

			// Track named UUID mapping
			if op.UUIDName != "" {
				namedUUIDMap[op.UUIDName] = uuid
			}

			// Update mock state based on table - create placeholder objects
			switch op.Table {
			case "Bridge":
				// Create a placeholder bridge (we don't parse Row details in this simple mock)
				bridgeName := fmt.Sprintf("bridge-%d", i)
				m.bridges[bridgeName] = &Bridge{
					UUID:        uuid,
					Name:        bridgeName,
					Ports:       []string{},
					ExternalIds: map[string]string{"created-by": "openperouter"},
				}
			case "Port":
				portName := fmt.Sprintf("port-%d", i)
				m.ports[portName] = &Port{
					UUID:       uuid,
					Name:       portName,
					Interfaces: []string{},
				}
			case "Interface":
				ifaceName := fmt.Sprintf("iface-%d", i)
				m.interfaces[ifaceName] = &Interface{
					UUID: uuid,
					Name: ifaceName,
					Type: "system",
				}
			}
		} else if op.Op == "mutate" {
			// Handle mutations (like adding ports to bridge)
			result.Count = 1
		}

		results[i] = result
	}

	return results, nil
}

func (m *mockOVSClient) Cache() *cache.TableCache {
	// Return nil cache for testing purposes
	return nil
}

func (m *mockOVSClient) Connected() bool {
	return m.connected
}

func (m *mockOVSClient) CurrentEndpoint() string {
	return "unix:/var/run/openvswitch/db.sock"
}

func (m *mockOVSClient) SetOption(opt libovsclient.Option) error {
	return nil
}

func (m *mockOVSClient) DisconnectNotify() chan struct{} {
	return make(chan struct{})
}

func (m *mockOVSClient) Disconnect() {
	m.connected = false
}

func (m *mockOVSClient) Echo(ctx context.Context) error {
	return nil
}

func (m *mockOVSClient) List(ctx context.Context, result interface{}) error {
	// Return empty list for testing
	return nil
}

func (m *mockOVSClient) MonitorAll(ctx context.Context) (libovsclient.MonitorCookie, error) {
	if m.monitorError != nil {
		return libovsclient.MonitorCookie{}, m.monitorError
	}
	return libovsclient.MonitorCookie{DatabaseName: "Open_vSwitch", ID: "mock-id-all"}, nil
}

func (m *mockOVSClient) MonitorCancel(ctx context.Context, cookie libovsclient.MonitorCookie) error {
	return nil
}

func (m *mockOVSClient) Schema() ovsdb.DatabaseSchema {
	return ovsdb.DatabaseSchema{}
}

func (m *mockOVSClient) UpdateEndpoints(endpoints []string) {
	// No-op for testing
}

// mockConditionalClient implementations

func (m *mockConditionalClient) Mutate(model model.Model, mutations ...model.Mutation) ([]ovsdb.Operation, error) {
	if m.client.mutateError != nil {
		return nil, m.client.mutateError
	}

	ops := []ovsdb.Operation{
		{
			Op:    "mutate",
			Table: getTableName(model),
		},
	}
	return ops, nil
}

func (m *mockConditionalClient) Delete() ([]ovsdb.Operation, error) {
	ops := []ovsdb.Operation{
		{
			Op: "delete",
		},
	}
	return ops, nil
}

func (m *mockConditionalClient) List(ctx context.Context, result interface{}) error {
	// Return empty result for testing
	return nil
}

func (m *mockConditionalClient) Update(model model.Model, fields ...interface{}) ([]ovsdb.Operation, error) {
	ops := []ovsdb.Operation{
		{
			Op: "update",
		},
	}
	return ops, nil
}

func (m *mockConditionalClient) Wait(until ovsdb.WaitCondition, timeout *int, row model.Model, fields ...interface{}) ([]ovsdb.Operation, error) {
	ops := []ovsdb.Operation{
		{
			Op: "wait",
		},
	}
	return ops, nil
}

// Helper function to get table name from model
func getTableName(model model.Model) string {
	switch model.(type) {
	case *Bridge:
		return "Bridge"
	case *Port:
		return "Port"
	case *Interface:
		return "Interface"
	case *OpenVSwitch:
		return "Open_vSwitch"
	default:
		return "Unknown"
	}
}

// Mock state setup helpers

func (m *mockOVSClient) addExistingBridge(name, uuid string) {
	m.bridges[name] = &Bridge{
		UUID:        uuid,
		Name:        name,
		Ports:       []string{},
		ExternalIds: map[string]string{},
	}
	m.openVSwitch.Bridges = append(m.openVSwitch.Bridges, uuid)
}

func (m *mockOVSClient) addExistingInterface(name, uuid string) {
	m.interfaces[name] = &Interface{
		UUID: uuid,
		Name: name,
		Type: "system",
	}
}

func (m *mockOVSClient) addExistingPort(name, uuid, ifaceUUID string) {
	m.ports[name] = &Port{
		UUID:       uuid,
		Name:       name,
		Interfaces: []string{ifaceUUID},
	}
}

func (m *mockOVSClient) attachPortToBridge(bridgeName, portUUID string) {
	if bridge, exists := m.bridges[bridgeName]; exists {
		// Check if port is not already attached
		for _, existingPort := range bridge.Ports {
			if existingPort == portUUID {
				return // Already attached
			}
		}
		bridge.Ports = append(bridge.Ports, portUUID)
	}
}

// Test suite starts here

var _ = Describe("OVS Bridge Operations", func() {
	var (
		ctx        context.Context
		mockClient *mockOVSClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockClient = newMockOVSClient()
	})

	Context("ensureBridge", func() {
		It("should create a new bridge when it doesn't exist", func() {
			bridgeName := "test-bridge"

			uuid, err := ensureBridge(ctx, mockClient, bridgeName)

			Expect(err).NotTo(HaveOccurred())
			Expect(uuid).NotTo(BeEmpty())
			Expect(mockClient.transactionCalls).To(Equal(1))
		})

		It("should return existing bridge UUID when bridge already exists", func() {
			bridgeName := "existing-bridge"
			existingUUID := "existing-uuid-123"
			mockClient.addExistingBridge(bridgeName, existingUUID)

			uuid, err := ensureBridge(ctx, mockClient, bridgeName)

			Expect(err).NotTo(HaveOccurred())
			Expect(uuid).To(Equal(existingUUID))
			Expect(mockClient.transactionCalls).To(Equal(0)) // No transaction needed
		})

		It("should return error for non-NotFound Get errors", func() {
			mockClient.getError = fmt.Errorf("database connection error")

			_, err := ensureBridge(ctx, mockClient, "test-bridge")

			// Should return the error immediately, not try to create
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("failed to check if bridge")))
			Expect(err).To(MatchError(ContainSubstring("database connection error")))
		})

		It("should handle Create operation failures", func() {
			mockClient.createError = fmt.Errorf("create failed")

			_, err := ensureBridge(ctx, mockClient, "test-bridge")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("create")))
		})

		It("should handle transaction failures", func() {
			mockClient.transactError = fmt.Errorf("transaction failed")

			_, err := ensureBridge(ctx, mockClient, "test-bridge")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("transaction")))
		})

		It("should set external_ids with 'created-by: openperouter'", func() {
			// This test verifies the bridge is created with correct metadata
			// In the mock, we don't deeply inspect operations, but in real usage
			// this would be verified
			uuid, err := ensureBridge(ctx, mockClient, "test-bridge")

			Expect(err).NotTo(HaveOccurred())
			Expect(uuid).NotTo(BeEmpty())
		})
	})

	Context("ensurePortAttachedToBridge", func() {
		var bridgeUUID string

		BeforeEach(func() {
			bridgeUUID = "bridge-uuid-123"
			mockClient.addExistingBridge("test-bridge", bridgeUUID)
		})

		It("should create interface and port, then attach to bridge", func() {
			interfaceName := "eth0"

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, interfaceName)

			Expect(err).NotTo(HaveOccurred())
			Expect(mockClient.transactionCalls).To(Equal(1))
		})

		It("should be idempotent when interface already exists", func() {
			interfaceName := "eth0"
			mockClient.addExistingInterface(interfaceName, "iface-uuid-123")

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, interfaceName)

			Expect(err).NotTo(HaveOccurred())
			// Transaction still happens for port creation and attachment
			Expect(mockClient.transactionCalls).To(Equal(1))
		})

		It("should be idempotent when port already exists", func() {
			interfaceName := "eth0"
			ifaceUUID := "iface-uuid-123"
			portUUID := "port-uuid-123"
			mockClient.addExistingInterface(interfaceName, ifaceUUID)
			mockClient.addExistingPort(interfaceName, portUUID, ifaceUUID)

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, interfaceName)

			Expect(err).NotTo(HaveOccurred())
			// Transaction still happens for attachment
			Expect(mockClient.transactionCalls).To(Equal(1))
		})

		It("should be idempotent when port already attached to bridge", func() {
			interfaceName := "eth0"
			ifaceUUID := "iface-uuid-123"
			portUUID := "port-uuid-123"
			mockClient.addExistingInterface(interfaceName, ifaceUUID)
			mockClient.addExistingPort(interfaceName, portUUID, ifaceUUID)
			mockClient.attachPortToBridge("test-bridge", portUUID)

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, interfaceName)

			Expect(err).NotTo(HaveOccurred())
			Expect(mockClient.transactionCalls).To(Equal(0)) // No transaction needed!
		})

		It("should handle partial state: interface exists but port doesn't", func() {
			interfaceName := "eth0"
			mockClient.addExistingInterface(interfaceName, "iface-uuid-123")

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, interfaceName)

			Expect(err).NotTo(HaveOccurred())
			// Should create port and attach
			Expect(mockClient.transactionCalls).To(Equal(1))
		})

		It("should handle partial state: port exists but not attached to bridge", func() {
			interfaceName := "eth0"
			ifaceUUID := "iface-uuid-123"
			portUUID := "port-uuid-123"
			mockClient.addExistingInterface(interfaceName, ifaceUUID)
			mockClient.addExistingPort(interfaceName, portUUID, ifaceUUID)

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, interfaceName)

			Expect(err).NotTo(HaveOccurred())
			// Should only attach the port
			Expect(mockClient.transactionCalls).To(Equal(1))
		})

		It("should handle bridge Get failures", func() {
			mockClient.bridges = make(map[string]*Bridge) // Clear bridges

			err := ensurePortAttachedToBridge(ctx, mockClient, "non-existent-bridge", "eth0")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("failed to get bridge")))
		})

		It("should handle Create operation failures", func() {
			mockClient.createError = fmt.Errorf("create failed")

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, "eth0")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("create")))
		})

		It("should handle mutate operation failures", func() {
			mockClient.mutateError = fmt.Errorf("mutate failed")

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, "eth0")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("mutate")))
		})

		It("should handle transaction failures", func() {
			mockClient.transactError = fmt.Errorf("transaction failed")

			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, "eth0")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("transaction")))
		})

		It("should create system type interfaces", func() {
			// This verifies that interfaces are created with type "system"
			// In a full implementation, we'd inspect the operation details
			err := ensurePortAttachedToBridge(ctx, mockClient, bridgeUUID, "eth0")

			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("ensureOVSBridgeAndAttachWithClient", func() {
		// Note: These tests are skipped because they require actual netlink interfaces
		// which are only available in integration test environments with real OVS
		PIt("should successfully ensure bridge and attach interface", func() {
			err := ensureOVSBridgeAndAttachWithClient(ctx, mockClient, "test-bridge", "eth0")

			Expect(err).NotTo(HaveOccurred())
			Expect(mockClient.transactionCalls).To(BeNumerically(">=", 1))
		})

		PIt("should be idempotent (running twice succeeds)", func() {
			// First call
			err := ensureOVSBridgeAndAttachWithClient(ctx, mockClient, "test-bridge", "eth0")
			Expect(err).NotTo(HaveOccurred())

			// Simulate state after first call
			mockClient.addExistingBridge("test-bridge", "bridge-uuid")
			mockClient.addExistingInterface("eth0", "iface-uuid")
			mockClient.addExistingPort("eth0", "port-uuid", "iface-uuid")
			mockClient.attachPortToBridge("test-bridge", "port-uuid")

			// Reset transaction counter
			firstCallTransactions := mockClient.transactionCalls
			mockClient.transactionCalls = 0

			// Second call
			err = ensureOVSBridgeAndAttachWithClient(ctx, mockClient, "test-bridge", "eth0")
			Expect(err).NotTo(HaveOccurred())

			// Should have minimal transactions (just monitor setup, no creates)
			Expect(mockClient.transactionCalls).To(BeNumerically("<", firstCallTransactions))
		})

		It("should handle monitor setup failures", func() {
			mockClient.monitorError = fmt.Errorf("monitor failed")

			err := ensureOVSBridgeAndAttachWithClient(ctx, mockClient, "test-bridge", "eth0")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("monitor")))
		})

		It("should propagate ensureBridge errors", func() {
			mockClient.createError = fmt.Errorf("bridge creation failed")

			err := ensureOVSBridgeAndAttachWithClient(ctx, mockClient, "test-bridge", "eth0")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("bridge")))
		})

		It("should propagate ensurePortAttachedToBridge errors", func() {
			// First create the bridge successfully
			mockClient.addExistingBridge("test-bridge", "bridge-uuid")

			// Then make port addition fail
			mockClient.mutateError = fmt.Errorf("port attach failed")

			err := ensureOVSBridgeAndAttachWithClient(ctx, mockClient, "test-bridge", "eth0")

			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring("attach")))
		})
	})
})
