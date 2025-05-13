# Image URL to use all building/pushing image targets
IMG ?= quay.io/openperouter/router:main
NAMESPACE ?= "openperouter-system"
LOGLEVEL ?= "info"
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.31.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_ENGINE ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=controller-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
	cp config/crd/bases/*.yaml charts/openperouter/charts/crds/templates

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v e2etest) -coverprofile cover.out
	sudo -E sh -c "umask 0; PATH=${GOPATH}/bin:$(pwd)/bin:${PATH} go test -tags=runasroot -v -race ./internal/hostnetwork"


##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/reloader cmd/reloader/main.go
	go build -o bin/controller cmd/hostcontroller/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

# If you wish to build the perouter image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
COMMIT := $(shell git describe --dirty --always)
BRANCH = $(shell git rev-parse --abbrev-ref HEAD)

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	@if [ "$(CONTAINER_ENGINE)" = "podman" ]; then \
		sudo $(CONTAINER_ENGINE) build --network=host -t ${IMG} .; \
	else \
		$(CONTAINER_ENGINE) build -t ${IMG} .; \
	fi

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	@if [ "$(CONTAINER_ENGINE)" = "podman" ]; then \
		sudo $(CONTAINER_ENGINE) push --network=host -t ${IMG} .; \
	else \
		$(CONTAINER_ENGINE) push -t ${IMG} .; \
	fi

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif


## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KIND ?= $(LOCALBIN)/kind
KUBECTL ?= $(LOCALBIN)/kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
GINKGO ?= $(LOCALBIN)/ginkgo
ENVTEST ?= $(LOCALBIN)/setup-envtest
HELM ?= $(LOCALBIN)/helm
KUBECONFIG_PATH ?= $(LOCALBIN)/kubeconfig
VALIDATOR_PATH ?= $(LOCALBIN)/validatehost
APIDOCSGEN ?= $(LOCALBIN)/crd-ref-docs
export KUBECONFIG=$(KUBECONFIG_PATH)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.0.0
CONTROLLER_TOOLS_VERSION ?= v0.14.0
KUBECTL_VERSION ?= v1.27.0
GINKGO_VERSION ?= v2.23.0
KIND_VERSION ?= v0.27.0
KIND_CLUSTER_NAME ?= pe-kind
HELM_VERSION ?= v3.12.3
HELM_DOCS_VERSION ?= v1.10.0
APIDOCSGEN_VERSION ?= v0.0.12

.PHONY: install
install: kubectl manifests kustomize ## Install CRDs into the K8s cluster specified in $KUBECONFIG_PATH.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: kind deploy-cluster deploy-controller ## Deploy cluster and controller.

.PHONY: deploy-with-prometheus
deploy-with-prometheus: KUSTOMIZE_LAYER=prometheus
deploy-with-prometheus: deploy-cluster deploy-prometheus deploy-controller

.PHONY: deploy-prometheus
deploy-prometheus: kubectl
	$(KUBECTL) apply --server-side -f hack/prometheus/manifests/setup
	until $(KUBECTL) get servicemonitors --all-namespaces ; do date; sleep 1; echo ""; done
	$(KUBECTL) apply -f hack/prometheus/manifests/
	$(KUBECTL) -n monitoring wait --for=condition=Ready --all pods --timeout 300s

.PHONY: deploy-cluster
deploy-cluster: kubectl manifests kustomize load-on-kind ## Deploy a cluster for the controller.

.PHONY: deploy-clab
deploy-clab: kubectl manifests kustomize load-on-kind ## Deploy a cluster for the controller.

KUSTOMIZE_LAYER ?= default
.PHONY: deploy-controller
deploy-controller: kubectl kustomize ## Deploy controller to the K8s cluster specified in $KUBECONFIG.
	cd config/pods && $(KUSTOMIZE) edit set image router=${IMG}
	$(KUBECTL) -n ${NAMESPACE} delete ds controller || true
	$(KUBECTL) -n ${NAMESPACE} delete ds router || true
	$(KUBECTL) -n ${NAMESPACE} delete deployment nodemarker || true

	# todo tweak loglevel
	$(KUSTOMIZE) build config/$(KUSTOMIZE_LAYER) | $(KUBECTL) apply -f -
	sleep 2s # wait for daemonset to be created
	$(KUBECTL) -n ${NAMESPACE} wait --for=condition=Ready --all pods --timeout 300s

.PHONY: deploy-helm
deploy-helm: helm kind deploy-cluster
	$(KUBECTL) -n ${NAMESPACE} delete ds controller || true
	$(KUBECTL) -n ${NAMESPACE} delete ds router || true
	$(KUBECTL) -n ${NAMESPACE} delete deployment nodemarker || true
	$(KUBECTL) create ns ${NAMESPACE} || true
	$(KUBECTL) label ns ${NAMESPACE} pod-security.kubernetes.io/enforce=privileged
	$(HELM) install openperouter charts/openperouter/ --set openperouter.image.tag=${IMG_TAG} \
	--set openperouter.image.pullPolicy=IfNotPresent --set openperouter.logLevel=debug --namespace ${NAMESPACE} $(HELM_ARGS)
	sleep 2s # wait for daemonset to be created
	$(KUBECTL) -n ${NAMESPACE} wait --for=condition=Ready --all pods --timeout 300s

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

KUSTOMIZE_INSTALL_SCRIPT ?= "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: kubectl
kubectl: $(KUBECTL) ## Download kubectl locally if necessary. If wrong version is installed, it will be overwritten.
$(KUBECTL): $(LOCALBIN)
	test -s $(LOCALBIN)/kubectl && $(LOCALBIN)/kubectl version --client | grep -q $(KUBECTL_VERSION) || \
	curl -o $(LOCALBIN)/kubectl -LO https://dl.k8s.io/release/$(KUBECTL_VERSION)/bin/$$(go env GOOS)/$$(go env GOARCH)/kubectl
	chmod +x $(LOCALBIN)/kubectl

.PHONY: helm
helm: $(HELM) ## Download helm locally if necessary. If wrong version is installed, it will be overwritten.
$(HELM): $(LOCALBIN)
	test -s $(LOCALBIN)/helm && $(LOCALBIN)/helm version | grep -q $(HELM_VERSION) || \
	USE_SUDO=false HELM_INSTALL_DIR=$(LOCALBIN) DESIRED_VERSION=$(HELM_VERSION) bash <(curl -s https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo locally if necessary. If wrong version is installed, it will be overwritten.
$(GINKGO): $(LOCALBIN)
	test -s $(LOCALBIN)/ginkgo && $(LOCALBIN)/ginkgo version | grep -q $(GINKGO_VERSION) || \
	GOBIN=$(LOCALBIN) go install github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)

kind: $(KIND) ## Download kind locally if necessary. If wrong version is installed, it will be overwritten.
$(KIND): $(LOCALBIN)
	test -s $(LOCALBIN)/kind && $(LOCALBIN)/kind --version | grep -q $(KIND_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

.PHONY:
crd-ref-docs: $(APIDOCSGEN) ## Download the api-doc-gen tool locally if necessary.
$(APIDOCSGEN): $(LOCALBIN)
	test -s $(LOCALBIN)/crd-ref-docs || \
	GOBIN=$(LOCALBIN) go install github.com/elastic/crd-ref-docs@$(APIDOCSGEN_VERSION)

.PHONY: e2etests 
e2etests: ginkgo kubectl build-validator create-export-logs
	$(GINKGO) -v $(GINKGO_ARGS) --timeout=3h ./e2etests -- --kubectl=$(KUBECTL) $(TEST_ARGS) --hostvalidator $(VALIDATOR_PATH) --reporterpath=${KIND_EXPORT_LOGS} 


.PHONY: clab-cluster
clab-cluster:
	KUBECONFIG_PATH=$(KUBECONFIG_PATH) KIND=$(KIND) clab/setup.sh
	@echo 'kind cluster created, to use it please'
	@echo 'export KUBECONFIG=${KUBECONFIG_PATH}'

.PHONY: load-on-kind
load-on-kind: clab-cluster ## Load the docker image into the kind cluster.
	KIND=$(KIND) bash -c 'source clab/common.sh && load_local_image_to_kind ${IMG} router'

.PHONY: lint
lint:
	hack/lint.sh

.PHONY: bumplicense
bumplicense:
	hack/bumplicense.sh

.PHONY: checkuncommitted
checkuncommitted:
	git diff --exit-code

.PHONY: bumpall
bumpall: bumplicense manifests
	go mod tidy

KIND_EXPORT_LOGS ?=/tmp/kind_logs

.PHONY: kind-export-logs
kind-export-logs: create-export-logs
	$(LOCALBIN)/kind export logs --name ${KIND_CLUSTER_NAME} ${KIND_EXPORT_LOGS}

.PHONY: generate-all-in-one
generate-all-in-one: manifests kustomize ## Create manifests
	cd config/pods && $(KUSTOMIZE) edit set image controller=${IMG}
	cd config/pods && $(KUSTOMIZE) edit set namespace $(NAMESPACE)

	$(KUSTOMIZE) build config/default > config/all-in-one/openpe.yaml
	$(KUSTOMIZE) build config/crio > config/all-in-one/crio.yaml

.PHONY: helm-docs
helm-docs:
	docker run --rm -v $$(pwd):/app -w /app jnorwood/helm-docs:$(HELM_DOCS_VERSION) helm-docs

.PHONY: api-docs
api-docs: crd-ref-docs
	$(APIDOCSGEN) --config hack/crd-ref-docs.yaml --max-depth 10 --source-path "./api" --renderer=markdown --output-path ./API-DOCS.md

.PHONY: bumpversion
bumpversion:
	hack/release/pre_bump.sh
	hack/release/bumpversion.sh

.PHONY: cutrelease
cutrelease: bumpversion generate-all-in-one helm-docs
	hack/release/release.sh

.PHONY: build-validator
build-validator: ginkgo ## Build Ginkgo test binary.
	CGO_ENABLED=0 $(GINKGO) build -tags=externaltests ./internal/hostnetwork
	mv internal/hostnetwork/hostnetwork.test $(VALIDATOR_PATH)

.PHONY: create-export-logs
create-export-logs:
	mkdir -p ${KIND_EXPORT_LOGS}
