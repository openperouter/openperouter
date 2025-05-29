#!/bin/bash
set -euo pipefail
rm -rf ./bindata/deployment
mkdir -p ./bindata/deployment
cp -rf ./charts/* ./bindata/deployment/

pushd ./bindata/deployment/openperouter

rm -rf charts
rm -f templates/rbac.yaml
rm -f templates/service-accounts.yaml
find . -type f -exec sed -i -e 's/{{ template "openperouter.fullname" . }}-//g' {} \;
find . -type f -exec sed -i -e 's/app.kubernetes.io\///g' {} \;

popd
