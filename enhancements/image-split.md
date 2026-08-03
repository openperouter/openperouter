# Separate Container Images for FRR and Grout

## Summary

OpenPERouter ships a single monolithic image (`quay.io/openperouter/router:main`)
containing Go binaries, FRR, and —in the grout variant— DPDK/grout binaries.
Every container uses this image regardless of what it needs.

This enhancement splits it into three purpose-built images:

1. **`openperouter/perouter`** — Go binaries only.
2. **FRR image** — user-provided, defaults to upstream `quay.io/frrouting/frr`.
3. **Grout image** — user-provided, defaults to upstream quay.io/grout/grout.

## Motivation

### Goals

- **User-swappable FRR**: plug in any FRR version via a single Helm value.
- **Smaller images**: each image carries only what it needs, hence a reduced 
  security attack surface.
- **Eliminate `main-grout` tag**: openperouter/perouter:main image would ship
  the grcli grout client or equivalent.
- **Independent release cadence**: FRR/grout versions can be bumped without
  rebuilding the perouter image.
- **Simplify CI lanes**: Avoid building multiple images in Github CI.

### Non-Goals

- Changing pod topology (same containers, different images).
- Changing the FRR config mechanism (frr-reload.py, vtysh).
- Multi-arch grout image (DPDK is x86_64-only).

## Proposal

### Container-to-Image Mapping

| Container | New Image | Notes |
|-----------|-----------|-------|
| controller | `perouter` | +grcli |
| hostbridge | `perouter` | |
| nodemarker | `perouter` | |
| operator | `perouter` | |
| reloader | FRR image | +reloader binary via init container |
| frr | FRR image | |
| grout | Grout image | only when datapath=grout |
| cp-frr-files | FRR image | |

### The Reloader Problem

The reloader invokes `frr-reload.py` and `vtysh`, which must match the
running FRR version. 
Solutions:

a. the reloader container runs the **FRR image**,
and the Go `reloader` binary is injected via an init container:

```yaml
initContainers:
  - name: cp-reloader
    image: <perouter image>
    command: ["cp", "/reloader", "/shared/reloader"]
    volumeMounts:
      - name: reloader-bin
        mountPath: /shared
containers:
  - name: reloader
    image: <frr image>
    command: ["/shared/reloader"]
    volumeMounts:
      - name: reloader-bin
        mountPath: /shared
```

b. The reloader uses the vtysh and frr-reload.py shipped in 
the openperouter Dockerfile (this requires verifying if vtysh is compatible with 
different versions of FRR).

### The Controller + grcli Problem

When grout is enabled, the controller needs `grcli` or an alternative
to configure a running instance of grout. 
Viable solutions:
a. the perouter image builds the grcli binaries.
  a1. It can't be built on Alpine Linux, hence we should move to a CentOS-based image.
b. the controller uses a Go native library to interact with Grout. 
  b1. Such library must be implemented from scratch and kept updated.

### The FRR / Grout version problem

When grout datapath is enabled, the FRR version must be the same as the 
one used to build the Grout FRR dataplane plugin (dplane_grout.so).
Solutions:
a. When the grout datapath is selected, the frr image deployed is identical to the
grout one.

## Design Details

### Helm Values

On kernel datapath:
```yaml
openperouter:
  image:
    repository: quay.io/openperouter/perouter
    tag: ""
  frr:
    image:
      repository: quay.io/frrouting/frr
      tag: "10.6.0"
  grout:
    image:
      repository: quay.io/grout/grout
      tag: "0.16.0"
```

On Grout datapath:
```yaml
openperouter:
  image:
    repository: quay.io/openperouter/perouter
    tag: ""
  frr:
    image:
      repository: quay.io/grout/grout
      tag: "0.16.0"
  grout:
    image:
      repository: quay.io/grout/grout
      tag: "0.16.0"
```


## Drawbacks

- **More init containers**: one extra in the router pod (`cp-reloader`), one
  in the controller pod when grout is enabled (`cp-grcli`).
- **User responsibility**: custom FRR images must include vtysh and
  frr-reload.py; grout mode requires `dplane_grout.so`.
- **Breaking change** for users who hardcode the current image name.

## Alternatives

**Run reloader inside the FRR container** — rejected: independent container
lifecycle is valuable for restartability and resource accounting.

**Share FRR tools via volume** — rejected: vtysh has dynamic library
dependencies (libfrr, libyang) that are fragile to copy across distros.

**Keep monolithic image, parameterize FRR version** — rejected: still
requires rebuild to change FRR, doesn't reduce image size, doesn't
eliminate `main-grout`.

