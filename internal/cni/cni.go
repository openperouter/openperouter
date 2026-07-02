// SPDX-License-Identifier:Apache-2.0

// Package cni provisions interfaces inside a network namespace by invoking
// CNI plugins programmatically (via containernetworking/cni libcni). It is
// used to create underlay interfaces directly in the persistent router netns
// from a CNI config embedded in the Underlay spec.
//
// The libcni result cache is the source of truth for what has been
// provisioned: an interface exists in the router netns if and only if its
// attachment is recorded in the cache. Add skips the plugin invocation when a
// cached attachment exists, and Del tears attachments down from the
// recorded cache entries, so teardown works even after the defining config is
// gone (e.g. the Underlay was deleted). The cache directory must be
// persistent across controller restarts for this contract to hold.
package cni

import (
	"fmt"

	"github.com/containernetworking/cni/libcni"
	"github.com/containernetworking/cni/pkg/version"
)

const (
	// minSupportedCNIVersion is the minimum CNI spec version accepted for the
	// embedded configs. Starting with 1.0.0 the spec only defines the conflist
	// format, so older single plugin configs are not supported.
	minSupportedCNIVersion = "1.0.0"

	// defaultCNIInterfaceName is the interface name used for a CNI-provisioned
	// underlay interface when the spec omits it. It matches the API-level default
	// applied by the CRD schema.
	DefaultInterfaceName = "net1"

	// containerIDPrefix is the stable base of the CNI container ID used for
	// underlay attachments in the router netns (the netns is well-known and
	// singular per node).
	containerIDPrefix = "perouter-underlay"
)

// ValidateConfig tells whether config parses as a CNI conflist of a
// supported spec version (>= 1.0.0).
func ValidateConfig(config []byte) error {
	conf, err := libcni.NetworkConfFromBytes(config)
	if err != nil {
		return err
	}
	ok, err := version.GreaterThanOrEqualTo(conf.CNIVersion, minSupportedCNIVersion)
	if err != nil {
		return fmt.Errorf("invalid cniVersion %q: %w", conf.CNIVersion, err)
	}
	if !ok {
		return fmt.Errorf("cniVersion %q is not supported, only CNI >= %s is", conf.CNIVersion, minSupportedCNIVersion)
	}
	return nil
}

// containerIDForNode returns the node-scoped CNI container ID for underlay
// attachments. Node-scoping keeps identifiers that plugins derive from the
// container ID (e.g. the dhcp plugin's client-id) unique per node.
func containerIDForNode(nodeName string) string {
	if nodeName == "" {
		return containerIDPrefix
	}
	return containerIDPrefix + "-" + nodeName
}
