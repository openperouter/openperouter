// SPDX-License-Identifier:Apache-2.0

//go:build runasroot
// +build runasroot

package hostnetwork

import (
	"context"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"
	libovsclient "github.com/ovn-kubernetes/libovsdb/client"
)

// createOVSBridge creates an OVS bridge for testing
func createOVSBridge(name string) {
	ctx := context.Background()
	ovs, err := NewOVSClient(ctx)
	Expect(err).NotTo(HaveOccurred())
	defer ovs.Close()

	_, err = ovs.Monitor(ctx, ovs.NewMonitor(
		libovsclient.WithTable(&OpenVSwitch{}),
		libovsclient.WithTable(&Bridge{}),
		libovsclient.WithTable(&Port{}),
		libovsclient.WithTable(&Interface{}),
	))
	Expect(err).NotTo(HaveOccurred())

	_, err = EnsureBridge(ctx, ovs, name)
	Expect(err).NotTo(HaveOccurred())
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
			exec.Command("ovs-vsctl", "del-br", br).Run()
		}
	}
}

// checkOVSBridgeExists validates that an OVS bridge exists
func checkOVSBridgeExists(g Gomega, bridgeName string) {
	bridge, err := getOVSBridge(bridgeName)
	g.Expect(err).NotTo(HaveOccurred(), "failed to get OVS bridge %q", bridgeName)
	g.Expect(bridge).NotTo(BeNil())
	g.Expect(bridge.Name).To(Equal(bridgeName))
}

// checkOVSBridgeDeleted validates that an OVS bridge does not exist
func checkOVSBridgeDeleted(g Gomega, bridgeName string) {
	_, err := getOVSBridge(bridgeName)
	g.Expect(err).To(HaveOccurred(), "OVS bridge %q should not exist", bridgeName)
}

// checkVethAttachedToOVSBridge validates that a veth is attached to an OVS bridge
func checkVethAttachedToOVSBridge(g Gomega, bridgeName, vethName string) {
	hasPort, err := ovsBridgeHasPort(bridgeName, vethName)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(hasPort).To(BeTrue(), "veth %s should be attached to OVS bridge %s", vethName, bridgeName)
}
