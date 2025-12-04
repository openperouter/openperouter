// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
	"github.com/vishvananda/netlink"
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

	bridgeUUID, err := EnsureBridge(ctx, ovs, name)
	if err != nil {
		return err
	}

	err = ensureInternalPortForBridge(ctx, ovs, bridgeUUID, name)
	if err != nil {
		return err
	}

	return waitForOVSInterface(name)
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

// waitForOVSInterface waits for an OVS interface to appear using netlink notifications
func waitForOVSInterface(name string) error {
	if _, err := netlink.LinkByName(name); err == nil {
		return nil
	}

	ch := make(chan netlink.LinkUpdate)
	done := make(chan struct{})
	defer close(done)

	if err := netlink.LinkSubscribe(ch, done); err != nil {
		return fmt.Errorf("failed to subscribe to link updates: %w", err)
	}

	timeout := time.After(5 * time.Second)
	for {
		select {
		case update := <-ch:
			if update.Link.Attrs().Name == name {
				return nil
			}
		case <-timeout:
			return fmt.Errorf("timeout waiting for OVS interface %s to appear", name)
		}
	}
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
