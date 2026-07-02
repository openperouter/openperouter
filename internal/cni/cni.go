// SPDX-License-Identifier:Apache-2.0

// Package cni provisions interfaces inside a network namespace by invoking
// CNI plugins programmatically (via containernetworking/cni libcni). It is
// used to create underlay interfaces directly in the persistent router netns
// from a CNI config embedded in the Underlay spec.
//
// The libcni result cache is the source of truth for what has been
// provisioned: an interface exists in the router netns if and only if its
// attachment is recorded in the cache. Add skips the plugin invocation when a
// cached attachment exists, and DelCached tears attachments down from the
// recorded cache entries, so teardown works even after the defining config is
// gone (e.g. the Underlay was deleted). The cache directory must be
// persistent across controller restarts for this contract to hold.
package cni

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/containernetworking/cni/libcni"
)

// Params holds everything needed to ADD a CNI network into a netns.
type Params struct {
	// Config is the CNI conf or conflist JSON.
	Config []byte
	// NetNS is the target network namespace path, e.g. /var/run/netns/perouter.
	NetNS string
	// IfName is the interface name to create inside the netns.
	IfName string
	// ContainerID is a stable identifier for the attachment (used by the
	// libcni result cache so DEL can find the cached result).
	ContainerID string
	// BinDirs are the directories searched for CNI plugin binaries.
	BinDirs []string
	// CacheDir is where libcni stores the result cache; it must be stable
	// across controller restarts so DEL works.
	CacheDir string
	// CapabilityArgs are the runtime parameters forwarded to the plugin as
	// capability arguments (the CNI runtimeConfig). libcni only delivers the
	// keys that the plugin declares in its "capabilities" config block;
	// undeclared keys are silently stripped.
	CapabilityArgs map[string]interface{}
}

// Add invokes CNI ADD for the configured network into Params.NetNS. It is
// idempotent through the libcni result cache: when an attachment for
// (ContainerID, IfName) is already cached the plugin invocation is skipped.
// It returns true when the plugin was actually invoked, false when the cached
// attachment was reused.
func Add(ctx context.Context, p Params) (bool, error) {
	cni := libcni.NewCNIConfigWithCacheDir(p.BinDirs, p.CacheDir, nil)
	cached, err := cachedAttachment(cni, p.ContainerID, p.IfName)
	if err != nil {
		return false, fmt.Errorf("failed to read cni cache for %q: %w", p.ContainerID, err)
	}
	if cached != nil {
		return false, nil
	}

	confList, err := confListFromBytes(p.Config)
	if err != nil {
		return false, err
	}
	if _, err := cni.AddNetworkList(ctx, confList, runtimeConf(p)); err != nil {
		return false, fmt.Errorf("cni add %q into %s: %w", confList.Name, p.NetNS, err)
	}
	return true, nil
}

// DelCached invokes CNI DEL for every attachment cached under containerID
// whose interface name is not in keepIfNames, using the config and netns
// recorded at ADD time. This lets the controller tear an interface down even
// after its defining config is gone (e.g. the underlay was removed). It is
// idempotent: no cached attachments means nothing to do. DEL failures do not
// stop the teardown of the remaining attachments; the errors are joined and
// returned.
func DelCached(ctx context.Context, binDirs []string, cacheDir, containerID string, keepIfNames ...string) error {
	cni := libcni.NewCNIConfigWithCacheDir(binDirs, cacheDir, nil)
	attachments, err := cni.GetCachedAttachments(containerID)
	if err != nil {
		return fmt.Errorf("failed to read cni cache for %q: %w", containerID, err)
	}

	var errs []error
	for _, attachment := range attachments {
		if slices.Contains(keepIfNames, attachment.IfName) {
			continue
		}
		confList, err := confListFromBytes(attachment.Config)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to parse cached cni config for network %q: %w", attachment.Network, err))
			continue
		}
		rt := &libcni.RuntimeConf{
			ContainerID:    attachment.ContainerID,
			NetNS:          attachment.NetNS,
			IfName:         attachment.IfName,
			CapabilityArgs: attachment.CapabilityArgs,
		}
		if err := cni.DelNetworkList(ctx, confList, rt); err != nil {
			errs = append(errs, fmt.Errorf("cni del %q (%s) from %s: %w", attachment.Network, attachment.IfName, attachment.NetNS, err))
		}
	}
	return errors.Join(errs...)
}

// ValidateConfig tells whether config parses as a CNI conf or conflist.
func ValidateConfig(config []byte) error {
	if _, err := confListFromBytes(config); err != nil {
		return err
	}
	return nil
}

// cachedAttachment returns the cached attachment for (containerID, ifName),
// or nil when there is none.
func cachedAttachment(cni *libcni.CNIConfig, containerID, ifName string) (*libcni.NetworkAttachment, error) {
	attachments, err := cni.GetCachedAttachments(containerID)
	if err != nil {
		return nil, err
	}
	for _, attachment := range attachments {
		if attachment.IfName == ifName {
			return attachment, nil
		}
	}
	return nil, nil
}

func runtimeConf(p Params) *libcni.RuntimeConf {
	return &libcni.RuntimeConf{
		ContainerID:    p.ContainerID,
		NetNS:          p.NetNS,
		IfName:         p.IfName,
		CapabilityArgs: p.CapabilityArgs,
	}
}

// confListFromBytes builds a libcni NetworkConfigList from a CNI config that
// is either a conflist (has "plugins") or a single-plugin conf.
func confListFromBytes(config []byte) (*libcni.NetworkConfigList, error) {
	probe := map[string]any{}
	if err := json.Unmarshal(config, &probe); err != nil {
		return nil, fmt.Errorf("invalid CNI config JSON: %w", err)
	}
	if _, isList := probe["plugins"]; isList {
		confList, err := libcni.ConfListFromBytes(config)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CNI conflist: %w", err)
		}
		return confList, nil
	}
	conf, err := libcni.ConfFromBytes(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse single CNI conf: %w", err)
	}
	return libcni.ConfListFromConf(conf)
}
