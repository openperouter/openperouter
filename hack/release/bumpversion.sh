#!/bin/bash
set -e

if [ -z "$OPENPE_VERSION" ]; then
    echo "must set the OPENPE_VERSION environment variable"
    exit -1
fi

sed -i "s/newTag:.*$/newTag: v$OPENPE_VERSION/" config/pods/kustomization.yaml

sed -i "s/version:.*$/version: $OPENPE_VERSION/" charts/openperouter/Chart.yaml
sed -i "s/appVersion:.*$/appVersion: v$OPENPE_VERSION/" charts/openperouter/Chart.yaml
sed -i "s/version:.*$/version: $OPENPE_VERSION/" charts/openperouter/charts/crds/Chart.yaml
sed -i "s/appVersion:.*$/appVersion: v$OPENPE_VERSION/" charts/openperouter/charts/crds/Chart.yaml
helm dep update charts/openperouter

# Update version in website main page
sed -i "s/OpenPERouter Version .*/OpenPERouter Version $OPENPE_VERSION/" website/content/_index.md

# Update version references in installation page
sed -i "s|openperouter/openperouter/.*/config/all-in-one/openpe.yaml|openperouter/openperouter/v$OPENPE_VERSION/config/all-in-one/openpe.yaml|g" website/content/docs/installation.md
sed -i "s|openperouter/openperouter/.*/config/all-in-one/crio.yaml|openperouter/openperouter/v$OPENPE_VERSION/config/all-in-one/crio.yaml|g" website/content/docs/installation.md
sed -i "s|github.com/openperouter/openperouter/config/default?ref=.*|github.com/openperouter/openperouter/config/default?ref=v$OPENPE_VERSION|g" website/content/docs/installation.md
sed -i "s|github.com/openperouter/openperouter/config/crio?ref=.*|github.com/openperouter/openperouter/config/crio?ref=v$OPENPE_VERSION|g" website/content/docs/installation.md

sed -i "s|value: \"quay.io/openperouter/router:main\"|value: \"quay.io/openperouter/router:v$OPENPE_VERSION\"|" operator/config/pods/env.yaml
sed -i "s|image: controller:main|image: controller:v$OPENPE_VERSION|" operator/config/pods/operator.yaml
sed -i "s/openperouter-operator.v0.0.0/openperouter-operator.v$OPENPE_VERSION/" operator/config/manifests/bases/openperouter-operator.clusterserviceversion.yaml
sed -i "s/version: 0.0.0/version: $OPENPE_VERSION/" operator/config/manifests/bases/openperouter-operator.clusterserviceversion.yaml

make bundle
