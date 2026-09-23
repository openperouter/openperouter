---
weight: 60
title: "Troubleshoot"
description: "How to troubleshoot OpenPERouter"
icon: "article"
date: "2026-07-12T10:03:22+02:00"
lastmod: "2026-07-12T10:03:22+02:00"
toc: true
---

This section explains how to troubleshoot OpenPERouter deployments.

## Inspect Tool

The `inspect` tool makes debugging OpenPERouter deployments easier by
collecting related objects and logs into a single directory, for inspection
or attached to a bug report.

It can be found at OpenPERouter repository 
[`tools/inspect`](https://github.com/openperouter/openperouter/tree/main/tools/inspect) 

### Output
The tool wire all collected info to output directory (default is `openperouter-inspect/`), including:
- `node_info/` - per-node network and routing infrastructure information (IP link state, VRFs, type 2 and 5 routes, etc.)
- `<openperouter namespace>/` - namespace objects and workload logs (pod logs, events, CRs, roles, etc.)
- `<namespace name>/` - per-namespace directory with config resources (Underlay, L3VNI, L2VNI, etc.)

Please refer to the tool 
[README](https://github.com/openperouter/openperouter/blob/main/tools/inspect/README.md) 
for more details.


### Systemd mode
When OpenPERouter runs in [systemd mode](../configuration/systemd-mode/),
the router state can't be collected through the cluster API because the router
container isn't managed by Kubernetes. 

The `inspect_host` tool can be used for inspecting such nodes.

It can be found at OpenPERouter repository
[`tools/inspect`](https://github.com/openperouter/openperouter/tree/main/tools/inspect)

#### Output
The tool store all collected info to output directory (default is `/openperouter-inspect-host`), including: 
- `router_info_podman.log` - router infrastructure information collected from podman (for Podman Quadlet containers)
- `router_info_crictl.log` - router infrastructure information collected using crictl (e.g.: from CRI-O, containerd)
- `root_netns_info.log` - root network namespace information
- `config_files.log` - static config resources collection log
- `configs/` - collected static config resources, YAML form

Please see the tool 
[README](https://github.com/openperouter/openperouter/blob/main/tools/inspect/README.md)
for more details.
