// SPDX-License-Identifier:Apache-2.0

package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/containernetworking/cni/libcni"
)

var (
	// invoker is the singleton
	invoker *Invoker
)

// Invoker invokes CNI plugins found in pluginDirs, recording the attachments
// in the libcni result cache under cacheDir, keyed by containerID.
type Invoker struct {
	cniConfig   *libcni.CNIConfig
	containerID string
}

// InitInvoker set the Invoker singleton with searching pluginDirs for plugin binaries and
// caching the results under cacheDir, keyed by containerID (the stable
// identifier of this invoker's attachments) generated from nodeName. The cache directory must be
// stable across controller restarts so Del keeps working for attachments
// created by previous invocations.
func InitInvoker(pluginDirs []string, cacheDir, nodeName string) {
	invoker = newInvoker(pluginDirs, cacheDir, nodeName)
}

func newInvoker(pluginDirs []string, cacheDir, nodeName string) *Invoker {
	return &Invoker{
		cniConfig:   libcni.NewCNIConfigWithCacheDir(pluginDirs, cacheDir, nil),
		containerID: containerIDForNode(nodeName),
	}
}

// GetInvoker returns the Invoker singleton, or nil when InitInvoker was never
// called.
func GetInvoker() *Invoker {
	return invoker
}

// UnsetInvoker clears the Invoker singleton. Intended for tests that need to
// restore the unconfigured state.
func UnsetInvoker() {
	invoker = nil
}

// AddParams holds everything needed to ADD a CNI network into a netns.
type AddParams struct {
	// Config is the CNI conf or conflist JSON.
	Config []byte
	// NetNS is the target network namespace path, e.g. /var/run/netns/perouter.
	NetNS string
	// IfName is the interface name to create inside the netns.
	IfName string
	// CapabilityArgs are the runtime parameters forwarded to the plugin as
	// capability arguments (the CNI runtimeConfig). libcni only delivers the
	// keys that the plugin declares in its "capabilities" config block;
	// undeclared keys are silently stripped.
	CapabilityArgs map[string]any
}

// ConfigMismatchError reports that the CNI configuration of an already
// provisioned interface changed. In-place CNI config changes are not
// supported: reconciling them would require a DEL/ADD cycle with
// partial-failure states where the old interface is torn down but the new one
// fails to provision.
type ConfigMismatchError struct {
	IfName string
}

func (e ConfigMismatchError) Error() string {
	return fmt.Sprintf("cni config for interface %q changed but in-place changes are not supported, "+
		"delete and recreate the Underlay to change it", e.IfName)
}

// validateCachedAttachment checks whether the cached attachment matches the
// requested config and capability arguments, returning a ConfigMismatchError
// when they differ.
func validateCachedAttachment(attachment *libcni.NetworkAttachment, p AddParams) error {
	sameConfig, err := jsonEqual(attachment.Config, p.Config)
	if err != nil {
		return fmt.Errorf("failed to compare cni config for %q: %w", p.IfName, err)
	}
	sameCapabilityArgs, err := capabilityArgsEqual(attachment.CapabilityArgs, p.CapabilityArgs)
	if err != nil {
		return fmt.Errorf("failed to compare cni capability args for %q: %w", p.IfName, err)
	}
	if !sameConfig || !sameCapabilityArgs {
		return ConfigMismatchError{IfName: p.IfName}
	}
	return nil
}

// Add invokes CNI ADD for the configured network into AddParams.NetNS. It is
// idempotent through the libcni result cache: when an attachment for
// (containerID, IfName) is already cached with the same config and capability
// arguments the plugin invocation is skipped. When the cached attachment was
// provisioned with a different config a ConfigMismatchError is returned: the
// existing interface is left untouched and the underlay must be deleted and
// recreated to change it. This backs the validation webhook's immutability
// check on configuration paths that bypass admission (e.g. host mode static
// files).
func (inv *Invoker) Add(ctx context.Context, p AddParams) error {
	cached, err := inv.cniConfig.GetCachedAttachments(inv.containerID)
	if err != nil {
		return fmt.Errorf("failed to read cni cache for %q: %w", inv.containerID, err)
	}

	idx := slices.IndexFunc(cached, func(n *libcni.NetworkAttachment) bool {
		return n.IfName == p.IfName
	})
	if idx >= 0 {
		return validateCachedAttachment(cached[idx], p)
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
		return fmt.Errorf("cni add %q into %s: %w", confList.Name, p.NetNS, err)
	}
	return nil
}

// Del invokes CNI DEL for the cached attachments matching the given
// interface names, using the config and netns recorded at ADD time. This
// lets the controller tear the interfaces down even after their defining
// config is gone (e.g. the underlay was removed). It is idempotent:
// interfaces without a cached attachment are skipped. DEL failures do not
// stop the teardown of the remaining attachments; the errors are joined and
// returned.
func (inv *Invoker) Del(ctx context.Context, ifNameToDelete string) error {
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

// CachedIfNames returns the interface names of the attachments recorded in
// the invoker's result cache, i.e. the interfaces currently managed through
// CNI.
func (inv *Invoker) CachedIfNames() ([]string, error) {
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

func (inv *Invoker) findCachedAttachmentByInterfaceName(ifNameToFind string) (*libcni.NetworkAttachment, error) {
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

// jsonEqual compares two JSON documents semantically, ignoring formatting and
// key ordering differences.
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

// capabilityArgsEqual compares two capability argument maps semantically
// through a JSON round-trip so value types are normalized. Nil and empty maps
// compare equal.
func capabilityArgsEqual(a, b map[string]any) (bool, error) {
	if len(a) == 0 && len(b) == 0 {
		return true, nil
	}
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false, fmt.Errorf("marshalling first capability args: %w", err)
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false, fmt.Errorf("marshalling second capability args: %w", err)
	}
	return jsonEqual(aJSON, bJSON)
}
