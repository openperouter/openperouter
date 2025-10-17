#!/bin/bash
set -e

if [ -z "$OPENPE_VERSION" ]; then
    echo "must set the OPENPE_VERSION environment variable"
    exit -1
fi


git add charts/openperouter/Chart.lock operator/bindata/deployment/openperouter/Chart.lock
git commit -a -m "Automated update for release v$OPENPE_VERSION"
git tag "v$OPENPE_VERSION" -m 'See the release notes for details:\n\nhttps://raw.githubusercontent.com/metallb/frr-k8s/main/RELEASE_NOTES.md'
git checkout main
