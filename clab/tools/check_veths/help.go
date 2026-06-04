// SPDX-License-Identifier:Apache-2.0

package main

import "fmt"

// printHelp displays usage information and examples.
func printHelp() {
	fmt.Println(`Usage: check_veths -f <name of configuration file>

Create and monitor veth pairs, attach them to containers or bridges and assign IP addresses whenever they are deleted.

IMPORTANT: For each veth pair, the left veth is the interface that is monitored and it must be attached to either:
  - bridge "leafkind-switch", OR
  - container "clab-kind-leafkind"
IMPORTANT: All links that shall be monitored must exist prior to starting this script.

  YAML Format for configuration file:

    interfaces:
    - left: <veth>
      right: <veth>
    - left: <veth>
      right: <veth>
  ...

  Each veth is specified as a YAML object with the following fields:
    - name      (string, required): interface name
    - container (string, required*): container name to attach to
    - bridge    (string, required*): bridge name to attach to
    - ips       (array, optional):  IP addresses to assign (e.g., ["192.168.1.1/24", "2001:db8::1/64"])

  * Either "container" OR "bridge" must be set (but not both).
    A veth attached to a bridge cannot have IP addresses assigned.

Examples:

  # Veth pair: bridge-attached to container-attached
  interfaces:
  - left:
      bridge: "leafkind-switch"
      name: "kindctrlpl"
    right:
      container: "pe-kind-control-plane"
      name: "toswitch"
      ips: ["192.168.11.3/24", "2001:db8:11::3/64"]


  # Veth pair: container-attached to container-attached
  interfaces:
  - left:
      container: "clab-kind-leafkind"
      name: "tokindworker"
    right:
      container: "pe-kind-worker"
      name: "toleafkind"

Environment Variables:
  CONTAINER_ENGINE_CLI: Container engine to use (default: "docker")

Parameters:
  -f <configuration file> (use '-' for STDIN)
  `)
}
