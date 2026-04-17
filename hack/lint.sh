#!/bin/bash
set -o errexit
set -x

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-2.9.0}"
CUSTOM_BINARY="golangci-lint"
CUSTOM_BINARY_PATH=${RELBIN}/${CUSTOM_BINARY}
TIMEOUT="10m0s"
CMD="run --timeout 10m0s ./..."
ENV="${ENV:-container}"

if [ "$ENV" == "container" ]; then
     docker run --rm -v $(git rev-parse --show-toplevel):/app -w /app golangci/golangci-lint:v$GOLANGCI_LINT_VERSION \
          sh -c "golangci-lint custom --name ${CUSTOM_BINARY} && ${CUSTOM_BINARY_PATH} run --timeout $TIMEOUT ./..."
else
     curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v"$GOLANGCI_LINT_VERSION"
     golangci-lint custom --name ${CUSTOM_BINARY}
     $CUSTOM_BINARY_PATH run --timeout $TIMEOUT ./...
fi
