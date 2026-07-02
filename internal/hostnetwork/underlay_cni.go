// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openperouter/openperouter/internal/cni"
	"github.com/openperouter/openperouter/internal/netnamespace"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

// UnderlayCNIParams holds the data needed to provision underlay interfaces
// through CNI plugins directly into the router network namespace.
type UnderlayCNIParams struct {
	// Interfaces are the CNI-provisioned underlay interfaces.
	Interfaces []CNIInterfaceParams `json:"interfaces"`
	// Runtime is the node-level CNI invocation environment.
	Runtime CNIRuntime `json:"runtime"`
}

// CNIInterfaceParams describes a single CNI-provisioned underlay interface.
type CNIInterfaceParams struct {
	// Config is the CNI conf or conflist JSON.
	Config []byte `json:"config"`
	// IfName is the interface name created inside the namespace.
	IfName string `json:"if_name"`
	// CapabilityArgs are the runtime parameters forwarded to the plugin as
	// capability arguments (the CNI runtimeConfig).
	CapabilityArgs map[string]interface{} `json:"capability_args,omitempty"`
}

// CNIRuntime holds the node-level settings used to invoke CNI plugins for
// underlay interfaces and to tear down their cached attachments. It is always
// available to the teardown paths, even when the underlay configuration that
// created the interfaces is gone.
type CNIRuntime struct {
	// BinDirs are the directories searched for CNI plugin binaries.
	BinDirs []string `json:"bin_dirs"`
	// CacheDir is libcni's result cache directory. It must outlive the
	// controller process so DEL keeps working across restarts; sharing the
	// router netns lifetime (i.e. cleared on reboot together with it) keeps
	// the cache aligned with the interfaces actually provisioned. It must be
	// dedicated to openperouter: another CNI runtime garbage collecting a
	// shared cache dir (e.g. /var/lib/cni) could purge the cached
	// attachments.
	CacheDir string `json:"cache_dir"`
	// ContainerID is the stable identifier for the underlay attachments in
	// the libcni result cache.
	ContainerID string `json:"container_id"`
}

// cniContainerIDPrefix is the stable base of the CNI container ID used for
// underlay attachments in the router netns (the netns is well-known and
// singular per node).
const cniContainerIDPrefix = "perouter-underlay"

// CNIContainerIDForNode returns the node-scoped CNI container ID for underlay
// attachments. Node-scoping keeps identifiers that plugins derive from the
// container ID (e.g. the dhcp plugin's client-id) unique per node.
func CNIContainerIDForNode(nodeName string) string {
	if nodeName == "" {
		return cniContainerIDPrefix
	}
	return cniContainerIDPrefix + "-" + nodeName
}

// RemoveCNIUnderlay invokes CNI DEL for every underlay attachment recorded in
// the CNI cache, releasing the interfaces and their IPAM allocations. The
// cache is the source of truth: attachments are torn down from the config and
// netns recorded at ADD time, so this works after the underlay configuration
// is gone, and it is a no-op when nothing was provisioned.
func RemoveCNIUnderlay(ctx context.Context, runtime CNIRuntime) error {
	if runtime.CacheDir == "" {
		return nil
	}
	if err := cni.DelCached(ctx, runtime.BinDirs, runtime.CacheDir, runtime.ContainerID); err != nil {
		return fmt.Errorf("failed to delete cni underlay interfaces: %w", err)
	}
	return nil
}

// setupCNIInterfaces reconciles the CNI-provisioned underlay interfaces in
// the namespace against params.CNI. Cached attachments whose interface is no
// longer desired are deleted first; desired interfaces are then added, with
// the libcni result cache providing idempotency (an interface is added if and
// only if its attachment is not cached). Every provisioned interface is
// marked with the underlay group ID so the existing detection and rebuild
// logic treats it like any other underlay link.
func setupCNIInterfaces(ctx context.Context, ns netns.NsHandle, params UnderlayParams) error {
	slog.DebugContext(ctx, "setup underlay", "step", "setting up cni interfaces")
	defer slog.DebugContext(ctx, "setup underlay", "step", "cni interfaces set up")

	runtime := params.CNI.Runtime
	desired := cniInterfaceNames(params.CNI.Interfaces)
	if err := cni.DelCached(ctx, runtime.BinDirs, runtime.CacheDir, runtime.ContainerID, desired...); err != nil {
		return fmt.Errorf("failed to delete stale cni underlay interfaces: %w", err)
	}

	for _, iface := range params.CNI.Interfaces {
		invoked, err := cni.Add(ctx, cni.Params{
			Config:         iface.Config,
			NetNS:          params.TargetNS,
			IfName:         iface.IfName,
			ContainerID:    runtime.ContainerID,
			BinDirs:        runtime.BinDirs,
			CacheDir:       runtime.CacheDir,
			CapabilityArgs: iface.CapabilityArgs,
		})
		if err != nil {
			return fmt.Errorf("failed to add cni underlay interface %s: %w", iface.IfName, err)
		}
		if !invoked {
			slog.DebugContext(ctx, "cni underlay interface already cached, skipping add", "interface", iface.IfName)
		}
		if err := markCNIInterfaceAsUnderlay(ns, iface.IfName); err != nil {
			return err
		}
	}
	return nil
}

// markCNIInterfaceAsUnderlay assigns the underlay group ID to the interface
// and brings it up.
func markCNIInterfaceAsUnderlay(ns netns.NsHandle, ifName string) error {
	return netnamespace.In(ns, func() error {
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to get cni underlay interface %s (its attachment is recorded in the cni cache): %w",
				ifName, err)
		}
		if link.Attrs().Group != underlayGroupID {
			if err := netlink.LinkSetGroup(link, int(underlayGroupID)); err != nil {
				return fmt.Errorf("failed to set group ID on cni underlay interface %s: %w", ifName, err)
			}
		}
		if err := netlink.LinkSetUp(link); err != nil {
			return fmt.Errorf("could not set cni underlay interface %s up: %w", ifName, err)
		}
		return nil
	})
}

// cniInterfaceNames returns the interface names of the CNI-provisioned
// underlay interfaces.
func cniInterfaceNames(interfaces []CNIInterfaceParams) []string {
	names := make([]string, 0, len(interfaces))
	for _, iface := range interfaces {
		names = append(names, iface.IfName)
	}
	return names
}
