#!/bin/bash
set -e

if [ -z "$PEROUTER_VERSION" ]; then
    echo "must set the PEROUTER_VERSION environment variable"
    exit -1
fi

sed -i "s/newTag:.*$/newTag: v$PEROUTER_VERSION/" config/pods/kustomization.yaml

sed -i "s/version:.*$/version: $PEROUTER_VERSION/" charts/openperouter/Chart.yaml
sed -i "s/appVersion:.*$/appVersion: v$PEROUTER_VERSION/" charts/openperouter/Chart.yaml
sed -i "s/version:.*$/version: $PEROUTER_VERSION/" charts/openperouter/charts/crds/Chart.yaml
sed -i "s/appVersion:.*$/appVersion: v$PEROUTER_VERSION/" charts/openperouter/charts/crds/Chart.yaml
helm dep update charts/openperouter

