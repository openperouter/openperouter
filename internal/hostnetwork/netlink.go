// SPDX-License-Identifier:Apache-2.0

package hostnetwork

import "github.com/vishvananda/netlink"

// NetlinkOption is a function applied to a Link after creation but before link-up.
type NetlinkOption func(link netlink.Link) error
