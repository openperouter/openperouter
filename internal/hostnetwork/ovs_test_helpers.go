// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"os/exec"
	"strings"

	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
)

// createOVSBridge creates an OVS bridge for testing
func createOVSBridge(name string) error {
	ctx := context.Background()
	ovs, err := NewOVSClient(ctx)
	if err != nil {
		return err
	}
	defer ovs.Close()

	_, err = ovs.Monitor(ctx, ovs.NewMonitor(
		libovsclient.WithTable(&OpenVSwitch{}),
		libovsclient.WithTable(&Bridge{}),
		libovsclient.WithTable(&Port{}),
		libovsclient.WithTable(&Interface{}),
	))
	if err != nil {
		return err
	}

	_, err = EnsureBridge(ctx, ovs, name)
	if err != nil {
		return err
	}

	return nil
}

// getOVSBridge retrieves an OVS bridge by name, returns error if not found
func getOVSBridge(name string) (*Bridge, error) {
	ctx := context.Background()
	ovs, err := NewOVSClient(ctx)
	if err != nil {
		return nil, err
	}
	defer ovs.Close()

	_, err = ovs.Monitor(ctx, ovs.NewMonitor(libovsclient.WithTable(&Bridge{})))
	if err != nil {
		return nil, err
	}

	bridge := &Bridge{Name: name}
	err = ovs.Get(ctx, bridge)
	if err != nil {
		return nil, err
	}
	return bridge, nil
}

// ovsBridgeHasPort checks if a port is attached to an OVS bridge
func ovsBridgeHasPort(bridgeName, portName string) (bool, error) {
	ctx := context.Background()
	ovs, err := NewOVSClient(ctx)
	if err != nil {
		return false, err
	}
	defer ovs.Close()

	_, err = ovs.Monitor(ctx, ovs.NewMonitor(
		libovsclient.WithTable(&Bridge{}),
		libovsclient.WithTable(&Port{}),
	))
	if err != nil {
		return false, err
	}

	bridge := &Bridge{Name: bridgeName}
	err = ovs.Get(ctx, bridge)
	if err != nil {
		return false, err
	}

	port := &Port{Name: portName}
	err = ovs.Get(ctx, port)
	if err != nil {
		return false, nil // Port doesn't exist
	}

	for _, portUUID := range bridge.Ports {
		if portUUID == port.UUID {
			return true, nil
		}
	}
	return false, nil
}

// cleanupOVSBridges removes all test OVS bridges
func cleanupOVSBridges() {
	cmd := exec.Command("ovs-vsctl", "list-br")
	output, err := cmd.Output()
	if err != nil {
		return // OVS not available
	}

	bridges := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, br := range bridges {
		if strings.HasPrefix(br, "br-hs-") || strings.HasPrefix(br, "test-ovs-") {
			_ = exec.Command("ovs-vsctl", "del-br", br).Run()
		}
	}
}
