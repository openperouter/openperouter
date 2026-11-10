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

- Podman with Quadlet support
- systemd
- The bootstrap and runtime router images available locally or through a registry reachable during startup. Missing images are pulled automatically.

## Setup

OpenPERouter ships a small one-shot unit, `systemdmode/openperouter-bootstrap.service`. At every boot it runs the `/bootstrap` entry point of the router image, which renders the runtime Quadlets and FRR daemon settings from `/var/lib/openperouter/boot.yaml`, and then starts them. Only the bootstrap unit is enabled; the runtime units are started by it.

Configure `/var/lib/openperouter/node-config.yaml` as described in
[Node Configuration]({{< ref "../configuration/systemd-mode.md" >}}#node-configuration).
Bootstrap does not create this file.

For a custom router image, edit `OPENPEROUTER_IMAGE` in
`systemdmode/openperouter-bootstrap.service` before installing the service file.
Set `image` in `boot.yaml` to the runtime image.

```bash
install -m 0644 systemdmode/openperouter-bootstrap.service /etc/systemd/system/
mkdir -p /var/lib/openperouter/configs
install -m 0600 systemdmode/bootstrap/boot.yaml.example /var/lib/openperouter/boot.yaml
# edit /var/lib/openperouter/boot.yaml
```

For offline boot, preload both image references into the host's rootful Podman
store with `sudo podman pull` while online or `sudo podman load -i <archive>`.
If both settings use the same image reference, preload it once.

```bash
systemctl daemon-reload
systemctl enable --now openperouter-bootstrap.service
```

### Redeploying

Configuration and image changes take effect at the next boot: edit `boot.yaml` and reboot the host.

Only the kernel datapath is supported by the bootstrap for now.

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
