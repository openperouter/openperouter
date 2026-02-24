set -o errexit
set -o nounset
set -o pipefail

LOCALBIN_DIR="$(pwd)/bin"
MODELGEN="${LOCALBIN_DIR}/modelgen"
MODELGEN_VERSION="v0.8.0"
MODELGEN_PKG="github.com/ovn-kubernetes/libovsdb/cmd/modelgen@${MODELGEN_VERSION}"

# generate ovsdb bindings
if [ ! -x "${MODELGEN}" ]; then
  echo "modelgen not found in ${LOCALBIN_DIR}, installing..."
  mkdir -p "${LOCALBIN_DIR}"
  GOBIN="${LOCALBIN_DIR}" go install "${MODELGEN_PKG}"
fi

export PATH="${LOCALBIN_DIR}:${PATH}"
go generate ./internal/ovsmodel
