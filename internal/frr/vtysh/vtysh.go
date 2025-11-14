// SPDX-License-Identifier:Apache-2.0

package vtysh

import (
	"encoding/json"
	"errors"
	"os/exec"
	"sort"
)

type Cli func(args string) (string, error)

func Run(args string) (string, error) {
	out, err := exec.Command("/usr/bin/vtysh", "-c", args).CombinedOutput()
	return string(out), err
}

var _ Cli = Run

func VRFs(frrCli Cli) ([]string, error) {
	vrfs, err := frrCli("show bgp vrf all json")
	if err != nil {
		return nil, err
	}
	parsedVRFs, err := ParseVRFs(vrfs)
	if err != nil {
		return nil, err
	}
	return parsedVRFs, nil
}

func ParseVRFs(vtyshRes string) ([]string, error) {
	vrfs := map[string]interface{}{}
	err := json.Unmarshal([]byte(vtyshRes), &vrfs)
	if err != nil {
		return nil, errors.Join(err, errors.New("parseVRFs: failed to parse vtysh response"))
	}
	res := make([]string, 0)
	for v := range vrfs {
		res = append(res, v)
	}
	sort.Strings(res)
	return res, nil
}
