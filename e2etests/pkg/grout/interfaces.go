// SPDX-License-Identifier:Apache-2.0

package grout

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	. "github.com/onsi/gomega"
	"github.com/openperouter/openperouter/e2etests/pkg/executor"
	"github.com/openperouter/openperouter/e2etests/pkg/openperouter"
)

type Interface struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Flags      []string `json:"flags"`
	Devargs    string   `json:"devargs"`
	VLANParent string   `json:"vlan_parent"`
	VLANID     int32    `json:"vlan_id"`
}

func Interfaces(g Gomega, router openperouter.RouterExecutor) []Interface {
	out, err := router.Exec("grcli", "--err-exit", "--json", "interface", "show")
	g.Expect(err).To(Succeed(), "router %s: %s", router.Name(), out)
	var interfaces []Interface
	g.Expect(json.Unmarshal([]byte(out), &interfaces)).To(Succeed(), out)
	return interfaces
}

// InterfaceForName returns the details of a single grout interface. Unlike the
// interface list, they include the type-specific fields such as devargs and
// the VLAN parent.
func InterfaceForName(g Gomega, router openperouter.RouterExecutor, name string) Interface {
	out, err := router.Exec("grcli", "--err-exit", "--json", "interface", "show", "name", name)
	g.Expect(err).To(Succeed(), "router %s, interface %s: %s", router.Name(), name, out)
	var iface Interface
	g.Expect(json.Unmarshal([]byte(out), &iface)).To(Succeed(), out)
	return iface
}

// TrunkPortName mirrors the name the controller gives to a trunk port: the
// last 10 characters of the PF name and the VF index in two hexadecimal
// digits.
func TrunkPortName(pfName string, vfIndex int32) string {
	return fmt.Sprintf("%s%02x", lastChars(pfName, 10), vfIndex)
}

// TrunkVLANName mirrors the name the controller gives to a VLAN sub-interface
// of a trunk: the trunk port name followed by the VLAN tag in three
// hexadecimal digits.
func TrunkVLANName(pfName string, vfIndex, tag int32) string {
	return TrunkPortName(pfName, vfIndex) + fmt.Sprintf("%03x", tag)
}

func lastChars(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// EmulatedVFPCIAddress returns the PCI address of an emulated VF netdev. The
// emulated VFs are virtio-net NICs, whose netdev device is a virtio child of
// the PCI device, so the address is the parent of the device link target.
func EmulatedVFPCIAddress(nodeExec executor.Executor, netdev string) (string, error) {
	// The path is not built with filepath.Join, which would drop "device/.."
	// before readlink resolves the symlink.
	out, err := nodeExec.Exec("readlink", "-f", "/sys/class/net/"+netdev+"/device/..")
	if err != nil {
		return "", fmt.Errorf("resolving PCI address of %s: %w: %s", netdev, err, out)
	}
	return path.Base(strings.TrimSpace(out)), nil
}
