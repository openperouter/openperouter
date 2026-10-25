// SPDX-License-Identifier:Apache-2.0

// Package poc contains the synthetic device inventory and static gateway
// addressing used by the Grout DRA proof of concept.
package poc

import (
	"fmt"
	"strconv"
	"strings"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/dynamic-resource-allocation/resourceslice"
)

const deviceCount = 8

type Catalog struct {
	driverName string
	poolName   string
}

func New(driverName, poolName string) Catalog {
	return Catalog{driverName: driverName, poolName: poolName}
}

func (c Catalog) Resources() resourceslice.DriverResources {
	devices := make([]resourceapi.Device, 0, deviceCount)
	for i := range deviceCount {
		devices = append(devices, resourceapi.Device{Name: fmt.Sprintf("vhu-%d", i)})
	}
	return resourceslice.DriverResources{Pools: map[string]resourceslice.Pool{
		c.poolName: {Slices: []resourceslice.Slice{{Devices: devices}}},
	}}
}

// GatewayCIDR maps a synthetic slot to the static gateway expected by the
// POC VM cloud-init configuration.
func (Catalog) GatewayCIDR(device string) (string, error) {
	index, err := strconv.Atoi(strings.TrimPrefix(device, "vhu-"))
	if err != nil || index < 0 || index >= deviceCount || device != fmt.Sprintf("vhu-%d", index) {
		return "", fmt.Errorf("invalid grout vhost device %q", device)
	}
	return fmt.Sprintf("192.169.%d.1/24", 20+index), nil
}
