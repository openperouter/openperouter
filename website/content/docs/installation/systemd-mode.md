---
weight: 10
title: "Systemd Mode"
description: "Deploy OpenPERouter on the host using Podman Quadlets and systemd"
icon: "article"
date: "2025-06-15T15:03:22+02:00"
lastmod: "2025-06-15T15:03:22+02:00"
toc: true
---

OpenPERouter can run outside of Kubernetes as a systemd service using Podman Quadlets. This allows OpenPERouter to start at boot time and establish overlay network connectivity before the Kubernetes cluster is up. The overlay provided by OpenPERouter can then serve as the foundation for all node network connectivity, including the network used by Kubernetes itself.

## Prerequisites

- Podman
- systemd

## Setup

Deploy the Quadlet unit files to `/etc/containers/systemd/` and start the service:

```bash
cp openperouter*.container /etc/containers/systemd/
systemctl daemon-reload
systemctl start openperouter
```

## Configuration

See the [Systemd Mode configuration guide]({{< ref "../configuration/systemd-mode.md" >}}) for details on node configuration, router configuration files, and how static and API server configuration are merged.

## Kubernetes-Side Deployment

When running OpenPERouter on the host via systemd, the Kubernetes cluster still needs a lightweight component: the **hostbridge** DaemonSet. The hostbridge pod runs on each node and exports API server credentials and configuration to the host, allowing the systemd-managed OpenPERouter to connect back to the Kubernetes API.

In this mode, the controller and router DaemonSets are **not** deployed -- they are replaced by the systemd-managed processes. The nodemarker runs in webhook-only mode.

### Helm

Set `openperouter.hostmode` to `true`:

```bash
helm install openperouter openperouter/openperouter --set openperouter.hostmode=true
```

Or in a values file:

```yaml
openperouter:
  hostmode: true
```

This deploys only the hostbridge DaemonSet and the nodemarker (in webhook-only mode), skipping the controller and router.

### Kustomize

Use the `hostmode` overlay instead of the default configuration:

```yaml
namespace: openperouter-system

resources:
  - github.com/openperouter/openperouter/config/hostmode?ref=main
```

This overlay includes the hostbridge DaemonSet and the nodemarker, but not the controller or router.

### Operator

The operator uses the same `hostmode` value. Set `openperouter.hostmode: true` in the operator's values to deploy the hostbridge DaemonSet instead of the controller and router.
