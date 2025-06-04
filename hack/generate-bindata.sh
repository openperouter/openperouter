#!/bin/bash
set -euo pipefail
rm -rf ./operator/bindata/deployment
mkdir -p ./operator/bindata/deployment
cp -rf ./charts/* ./operator/bindata/deployment/

pushd ./operator/bindata/deployment/openperouter

rm -rf charts
rm -f templates/rbac.yaml
rm -f templates/service-accounts.yaml
find . -type f -exec sed -i -e 's/{{ template "openperouter.fullname" . }}-//g' {} \;
find . -type f -exec sed -i -e 's/app.kubernetes.io\///g' {} \;

popd
