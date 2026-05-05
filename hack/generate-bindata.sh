#!/bin/bash
set -euo pipefail
if [[ -d ./operator/bindata/deployment ]] && ! rm -rf ./operator/bindata/deployment 2>/dev/null; then
    # Files may be owned by another user (e.g. nobody from a container).
    # Move to a temp dir (works because the parent dir is user-owned) and
    # clean up in a container that can remove them.
    stale_dir=$(mktemp -d)
    mv ./operator/bindata/deployment "$stale_dir/"
    rm -rf "$stale_dir" 2>/dev/null || true
fi
mkdir -p ./operator/bindata/deployment
cp -rf --no-preserve=ownership ./charts/* ./operator/bindata/deployment/

pushd ./operator/bindata/deployment/openperouter

rm -rf charts
rm -f templates/rbac.yaml
rm -f templates/service-accounts.yaml
find . -type f -exec sed -i -e 's/{{ template "openperouter.fullname" . }}-//g' {} \;
find . -type f -exec sed -i -e 's/app.kubernetes.io\///g' {} \;

popd
