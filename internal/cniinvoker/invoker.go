// SPDX-License-Identifier:Apache-2.0

package cniinvoker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/containernetworking/cni/libcni"
)

// Invoker is the node-level CNI plugin invoker singleton, nil until Init is
// called.
var Invoker *invoker

// invoker invokes CNI plugins found in pluginDirs, recording the attachments
// in the libcni result cache under cacheDir, keyed by containerID.
type invoker struct {
	cniConfig   *libcni.CNIConfig
	containerID string
}

// Init sets the Invoker singleton. The cache directory must be stable across
// controller restarts so Del keeps working for attachments created by
// previous invocations.
func Init(pluginDirs []string, cacheDir, nodeName string) {
	Invoker = &invoker{
		cniConfig:   libcni.NewCNIConfigWithCacheDir(pluginDirs, cacheDir, nil),
		containerID: containerIDForNode(nodeName),
	}
}

// AddParams holds everything needed to ADD a CNI network into a netns.
type AddParams struct {
	// Config is the CNI conf or conflist JSON.
	Config []byte
	// NetNS is the target network namespace path, e.g. /var/run/netns/perouter.
	NetNS string
	// IfName is the interface name to create inside the netns.
	IfName string
	// CapabilityArgs is the CNI runtimeConfig forwarded to the plugin; the
	// values must be JSON-decoded (json.Unmarshal output).
	CapabilityArgs map[string]any
}

// ConfigMismatchError reports that the CNI configuration of an already
// provisioned interface changed; in-place changes are not supported.
type ConfigMismatchError struct {
	IfName string
}

func (e ConfigMismatchError) Error() string {
	return fmt.Sprintf("cni config for interface %q changed but in-place changes are not supported, "+
		"delete and recreate the Underlay to change it", e.IfName)
}

// Add invokes CNI ADD for the configured network into AddParams.NetNS. It is
// idempotent through the libcni result cache: a cached attachment with the
// same config skips the invocation, one with a different config returns a
// ConfigMismatchError leaving the interface untouched.
func (inv *invoker) Add(ctx context.Context, p AddParams) error {
	cached, err := inv.cniConfig.GetCachedAttachments(inv.containerID)
	if err != nil {
		return fmt.Errorf("failed to read cni cache for %q: %w", inv.containerID, err)
	}

	idx := slices.IndexFunc(cached, func(n *libcni.NetworkAttachment) bool {
		return n.IfName == p.IfName
	})
	if idx >= 0 {
		attachment := cached[idx]
		sameConfig, err := jsonEqual(attachment.Config, p.Config)
		if err != nil {
			return fmt.Errorf("failed to compare cni config for %q: %w", p.IfName, err)
		}
		if !sameConfig || !capabilityArgsEqual(attachment.CapabilityArgs, p.CapabilityArgs) {
			return ConfigMismatchError{IfName: p.IfName}
		}
		return nil
	}

	confList, err := libcni.NetworkConfFromBytes(p.Config)
	if err != nil {
		return err
	}

	rt := &libcni.RuntimeConf{
		ContainerID:    inv.containerID,
		NetNS:          p.NetNS,
		IfName:         p.IfName,
		CapabilityArgs: p.CapabilityArgs,
	}

	if _, err := inv.cniConfig.AddNetworkList(ctx, confList, rt); err != nil {
		addErr := fmt.Errorf("cni add %q into %s: %w", confList.Name, p.NetNS, err)
		// Per the CNI spec, a failed ADD must be followed by a DEL to clean
		// up any partially created resources, e.g. the interface itself.
		if delErr := inv.cniConfig.DelNetworkList(ctx, confList, rt); delErr != nil {
			return errors.Join(addErr, fmt.Errorf("cni del after failed add: %w", delErr))
		}
		return addErr
	}
	return nil
}

// Del invokes CNI DEL for the cached attachment matching the interface name,
// using the config and netns recorded at ADD time so teardown works after the
// defining config is gone. Interfaces without a cached attachment are
// skipped.
func (inv *invoker) Del(ctx context.Context, ifNameToDelete string) error {
	attachmentToDelete, err := inv.findCachedAttachmentByInterfaceName(ifNameToDelete)
	if err != nil {
		return fmt.Errorf("failed finding cni attachment with ifname %q to delete: %w", ifNameToDelete, err)
	}
	if attachmentToDelete == nil {
		return nil
	}
	confListToDelete, err := libcni.NetworkConfFromBytes(attachmentToDelete.Config)
	if err != nil {
		return fmt.Errorf("failed to parse cached cni config for network %q: %w", attachmentToDelete.Network, err)
	}
	if err := inv.cniConfig.DelNetworkList(ctx, confListToDelete, &libcni.RuntimeConf{
		ContainerID:    attachmentToDelete.ContainerID,
		NetNS:          attachmentToDelete.NetNS,
		IfName:         attachmentToDelete.IfName,
		CapabilityArgs: attachmentToDelete.CapabilityArgs,
	}); err != nil {
		return fmt.Errorf("cni del %q (%s) from %s: %w", attachmentToDelete.Network, attachmentToDelete.IfName, attachmentToDelete.NetNS, err)
	}
	return nil
}

// CachedIfNames returns the interface names recorded in the result cache,
// i.e. the interfaces currently managed through CNI.
func (inv *invoker) CachedIfNames() ([]string, error) {
	attachments, err := inv.cniConfig.GetCachedAttachments(inv.containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to read cni cache for %q: %w", inv.containerID, err)
	}
	names := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		names = append(names, attachment.IfName)
	}
	return names, nil
}

func (inv *invoker) findCachedAttachmentByInterfaceName(ifNameToFind string) (*libcni.NetworkAttachment, error) {
	attachments, err := inv.cniConfig.GetCachedAttachments(inv.containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to read cni cache for %q: %w", inv.containerID, err)
	}

	for _, attachment := range attachments {
		if attachment.IfName == ifNameToFind {
			return attachment, nil
		}
	}
	return nil, nil
}

// jsonEqual compares two JSON documents semantically.
func jsonEqual(a, b []byte) (bool, error) {
	var aVal, bVal any
	if err := json.Unmarshal(a, &aVal); err != nil {
		return false, fmt.Errorf("unmarshalling first document: %w", err)
	}
	if err := json.Unmarshal(b, &bVal); err != nil {
		return false, fmt.Errorf("unmarshalling second document: %w", err)
	}
	return reflect.DeepEqual(aVal, bVal), nil
}

// capabilityArgsEqual compares two JSON-decoded capability argument maps;
// nil and empty maps compare equal.
func capabilityArgsEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
