# OpenShift image build

First, register the subscription
```
$ subscription-manager register --force --username ... --password ...

# or

$ subscription-manager register --force --org ... --activationkey ...
```

Then, build the image with
```
# Share the entitlement with the build
TMPDIR=$(mktemp -d)
cp -r /etc/pki/entitlement "$TMPDIR/entitlement"
cp -r /etc/rhsm "$TMPDIR/rhsm"

# Build the image
podman build -v "$TMPDIR/entitlement:/run/secrets/etc-pki-entitlement:Z"  \
               -v "$TMPDIR/rhsm:/run/secrets/rhsm:Z" \
               --build-arg BASE_IMAGE=registry.redhat.io/ubi10/ubi:10.1-1774545609 \
               -f Dockerfile.edge.openshift .
```

## Refreshing RPM lockfiles

The `rpms.in.yaml` and `rpms.lock.yaml` files declare the RPM dependencies
needed by the OpenShift images. Konflux uses them to prefetch packages for
hermetic builds.

FRR is consumed from Fast Datapath (FDP): the `frr` daemon (runtime) and
`frr-headers` (used to compile the `grout-frr` zebra dataplane plugin) come from
the `fast-datapath-for-rhel-10-x86_64-rpms` repository, not from a source build.

## When to refresh

Update these files whenever:

- A package is added or removed in `Dockerfile.edge.openshift` (the `dnf install`
  lines in the `grout-builder` stage).
- You want to pick up newer package versions of `registry.redhat.io/ubi10/ubi`.

### Steps

1. **Edit `rpms.in.yaml`** — add or remove entries in the `packages` list to
   match the packages installed by `dnf` in the `grout-builder` stage.

2. **Regenerate `rpms.lock.yaml`**:
 Follow instructions at 
 https://konflux-ci.dev/docs/building/activation-keys-subscription/#configuring-an-rpm-lockfile-for-hermetic-builds

 
```bash
# Share the entitlement with podman
TMPDIR=$(mktemp -d)
cp -r /etc/pki/entitlement "$TMPDIR/entitlement"
cp -r /etc/rhsm "$TMPDIR/rhsm"

podman run -it -v `pwd`:/src:Z -v "$TMPDIR/entitlement:/run/secrets/etc-pki-entitlement:Z"  \
               -v "$TMPDIR/rhsm:/run/secrets/rhsm:Z" registry.redhat.io/ubi10/ubi:10.1-1774545609 bash

dnf install -y pip skopeo
pip install https://github.com/konflux-ci/rpm-lockfile-prototype/archive/refs/tags/v0.13.1.tar.gz

dnf config-manager --set-enabled "codeready-builder-for-rhel-10-x86_64-rpms,rhel-10-for-x86_64-baseos-rpms,rhel-10-for-x86_64-appstream-rpms,fast-datapath-for-rhel-10-x86_64-rpms";

# clean redhat.repo by removing all the disabled repositories
awk 'BEGIN{RS=""; ORS="\n\n"} /^#/ || /enabled = 1/' /etc/yum.repos.d/redhat.repo > /src/openshift/redhat.repo

cp /run/secrets/etc-pki-entitlement/* /etc/pki/entitlement/
skopeo login registry.redhat.io

cd /src; rpm-lockfile-prototype --debug --bare --outfile openshift/rpms.lock.yaml openshift/rpms.in.yaml
```

3. **Commit both files** together.
