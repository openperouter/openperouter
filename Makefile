# Image URL to use all building/pushing image targets
IMG_TAG ?= main
IMG_REPO ?= quay.io/openperouter
IMG_NAME ?= router
IMG ?= $(IMG_REPO)/$(IMG_NAME):$(IMG_TAG)
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

# CLAB_TOPOLOGY_FILE allows the user to specify which containerlab topology
# file to deploy. It defauls to the single cluster variant
CLAB_TOPOLOGY_FILE ?= singlecluster/kind.clab.yml

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
	$(CONTROLLER_GEN) crd webhook paths="./api/..." paths="./config/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) crd webhook paths="./operator/api/..." paths="./operator/config/..." output:crd:artifacts:config=operator/config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=controller-role paths="./internal/controller/..." output:rbac:artifacts:config=config/rbac/
	$(CONTROLLER_GEN) rbac:roleName=operator-role paths="./operator/..." output:rbac:artifacts:config=operator/config/rbac/
	cp config/crd/bases/*.yaml charts/openperouter/charts/crds/templates
	rm -f charts/openperouter/charts/crds/templates/kustomization.yaml
	hack/generate-bindata.sh

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
test: fmt vet envtest $(LOCALBIN) ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test $$(go list ./... | grep -v e2etest) -coverprofile cover.out
	@RUNASROOT_TESTS=""; \
	for pkg in $$(grep -rl "//go:build runasroot" --include="*_test.go" . | xargs -I{} dirname {} | sort -u); do \
		name=$$(basename $$pkg); \
		go test -tags=runasroot -c -race -o $(LOCALBIN)/$$name.test $$pkg; \
		RUNASROOT_TESTS="$$RUNASROOT_TESTS /src/bin/$$name.test"; \
	done; \
	$(CONTAINER_ENGINE) run --rm --privileged -v $$(pwd):/src -w /src --entrypoint /src/hack/integration_tests.sh $(KIND_NODE_IMG) $$RUNASROOT_TESTS

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/reloader ./cmd/reloader
	go build -o bin/controller ./cmd/hostcontroller
	go build -o bin/hostbridge ./cmd/hostbridge
	go build -o bin/nodemarker ./cmd/nodemarker

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
		sudo $(CONTAINER_ENGINE) build  -t ${IMG} .; \
	else \
		$(CONTAINER_ENGINE) build -t ${IMG} .; \
	fi


TLS_VERIFY ?= "true"
.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	@if [ "$(CONTAINER_ENGINE)" = "podman" ]; then \
		sudo $(CONTAINER_ENGINE) push --tls-verify=${TLS_VERIFY} ${IMG}; \
	else \
		$(CONTAINER_ENGINE) push ${IMG}; \
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
HUGO ?= $(LOCALBIN)/hugo
export KUBECONFIG=$(KUBECONFIG_PATH)

## Tool Versions
KUSTOMIZE_VERSION ?= v5.0.0
CONTROLLER_TOOLS_VERSION ?= v0.19.0
KUBECTL_VERSION ?= v1.27.0
GINKGO_VERSION ?= $(shell go list -m -f '{{.Version}}' github.com/onsi/ginkgo/v2)
KIND_VERSION ?= v0.27.0
KIND_CLUSTER_NAME ?= pe-kind
HELM_VERSION ?= v3.12.3
HELM_DOCS_VERSION ?= v1.10.0
APIDOCSGEN_VERSION ?= v0.0.12
HUGO_VERSION ?= v0.147.8

# Kind node image configuration
KIND_NODE_VERSION ?= v1.32.2
KIND_NODE_IMG_REPO ?= quay.io/openperouter
KIND_NODE_IMG_NAME ?= kind-node-openperouter
KIND_NODE_IMG_TAG ?= $(KIND_NODE_VERSION)
KIND_NODE_IMG ?= $(KIND_NODE_IMG_REPO)/$(KIND_NODE_IMG_NAME):$(KIND_NODE_IMG_TAG)
export NODE_IMAGE ?= $(KIND_NODE_IMG)

.PHONY: install
install: kubectl manifests kustomize ## Install CRDs into the K8s cluster specified in $KUBECONFIG_PATH.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: kind deploy-cluster deploy-controller ## Deploy cluster and controller.

.PHONY: setup-hostmode
setup-hostmode: ## Setup node configuration for hostmode.
	./systemdmode/setup_node_config.sh $(KIND_CLUSTER_NAME)

.PHONY: deploy-hostmode
deploy-hostmode: export KUSTOMIZE_LAYER=hostmode
deploy-hostmode: kind deploy-cluster setup-hostmode deploy-controller ## Deploy cluster and controller in hostmode, then setup systemd services.
	./systemdmode/deploy.sh $(KIND_CLUSTER_NAME)

.PHONY: setup-hostmode-boot
setup-hostmode-boot: ## Setup node configuration for hostmode with static config files.
	NODE_CONFIG_DIR=e2etests/systemd_static_suite/testdata ./systemdmode/setup_node_config.sh $(KIND_CLUSTER_NAME)

.PHONY: deploy-hostmode-boot
deploy-hostmode-boot: export KUSTOMIZE_LAYER=hostmode
deploy-hostmode-boot: kind deploy-cluster setup-hostmode-boot ## Deploy cluster in hostmode with static config, without deploying controller (boot mode).
	./systemdmode/deploy.sh $(KIND_CLUSTER_NAME)

.PHONY: deploy-multi
deploy-multi: kind deploy-multi-cluster deploy-controller-multi ## Deploy cluster and controller.

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
deploy-cluster: kubectl manifests kustomize clab-cluster load-on-kind ## Deploy a cluster for the controller.

.PHONY: deploy-clab
deploy-clab: kubectl manifests kustomize clab-cluster load-on-kind ## Deploy a cluster for the controller.

.PHONY: deploy-multi-cluster
deploy-multi-cluster: kubectl manifests kustomize clab-multi-cluster load-on-multi-cluster ## Deploy multi-cluster setup for the controller.

KUSTOMIZE_LAYER ?= default
.PHONY: deploy-controller
deploy-controller: kubectl kustomize ## Deploy controller to the K8s cluster specified in $KUBECONFIG.
	$(MAKE) deploy-controller-cluster CLUSTER_NAME=default CLUSTER_KUBECONFIG=${KUBECONFIG_PATH}

.PHONY: deploy-controller-multi
deploy-controller-multi: kubectl kustomize ## Deploy controller to both clusters in multi-cluster setup.
	$(MAKE) deploy-controller-cluster CLUSTER_NAME=pe-kind-a CLUSTER_KUBECONFIG=${KUBECONFIG_PATH}-pe-kind-a
	$(MAKE) deploy-controller-cluster CLUSTER_NAME=pe-kind-b CLUSTER_KUBECONFIG=${KUBECONFIG_PATH}-pe-kind-b

.PHONY: deploy-controller-cluster
deploy-controller-cluster: kubectl kustomize ## Deploy controller to a specific cluster (internal target).
	@echo "=== Deploying controller to cluster $(CLUSTER_NAME) ==="
	cd config/pods && $(KUSTOMIZE) edit set image router=${IMG}
	KUBECONFIG=$(CLUSTER_KUBECONFIG) $(KUBECTL) -n ${NAMESPACE} delete ds controller || true
	KUBECONFIG=$(CLUSTER_KUBECONFIG) $(KUBECTL) -n ${NAMESPACE} delete ds router || true
	KUBECONFIG=$(CLUSTER_KUBECONFIG) $(KUBECTL) -n ${NAMESPACE} delete deployment nodemarker || true

	# todo tweak loglevel
	$(KUSTOMIZE) build config/$(KUSTOMIZE_LAYER) | KUBECONFIG=$(CLUSTER_KUBECONFIG) $(KUBECTL) apply -f -
	sleep 2s # wait for daemonset to be created
	KUBECONFIG=$(CLUSTER_KUBECONFIG) $(KUBECTL) -n ${NAMESPACE} wait --for=condition=Ready --all pods --timeout 300s
	@echo "=== Controller deployed successfully to cluster $(CLUSTER_NAME) ==="

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
	$(GINKGO) -v $(GINKGO_ARGS) --timeout=3h ./e2etests/suite -- --kubectl=$(KUBECTL) $(TEST_ARGS) --hostvalidator $(VALIDATOR_PATH) --reporterpath=${KIND_EXPORT_LOGS}

.PHONY: e2etests-hostmode-boot
e2etests-hostmode-boot: ginkgo kubectl build-validator create-export-logs ## Run e2e tests for hostmode boot scenario (static config first, then K8s API).
	@echo "=== Running systemd_static_suite tests (static config only) ==="
	$(GINKGO) -v $(GINKGO_ARGS) --timeout=3h ./e2etests/systemd_static_suite -- --kubectl=$(KUBECTL) $(TEST_ARGS)
	@echo "=== Deploying controller to enable K8s API ==="
	$(MAKE) deploy-controller KUSTOMIZE_LAYER=hostmode
	@echo "=== Running passthrough tests (with K8s API available) ==="
	$(GINKGO) -v $(GINKGO_ARGS) --timeout=3h --label-filter='passthrough' ./e2etests/suite -- --kubectl=$(KUBECTL) $(TEST_ARGS) --skip-underlay-passthrough --systemdmode --hostvalidator $(VALIDATOR_PATH) --reporterpath=${KIND_EXPORT_LOGS} 


.PHONY: clab-cluster
clab-cluster: kind-node-image-build
	KUBECONFIG_PATH=$(KUBECONFIG_PATH) KIND=$(KIND) CLAB_TOPOLOGY=$(CLAB_TOPOLOGY_FILE) clab/setup.sh
	@echo 'kind cluster created, to use it please'
	@echo 'export KUBECONFIG=${KUBECONFIG_PATH}'

.PHONY: clab-multi-cluster
clab-multi-cluster: kind-node-image-build ## Deploy multi-cluster setup with 2 kindleafs, 2 kind switches, and 2 kind clusters (control plane + worker each)
	KUBECONFIG_PATH=$(KUBECONFIG_PATH) KIND=$(KIND) CLAB_TOPOLOGY=multicluster/kind.clab.yml clab/setup.sh pe-kind-a pe-kind-b
	@echo 'Multi-cluster deployment created:'
	@echo '  - Cluster A: export KUBECONFIG=${KUBECONFIG_PATH}-pe-kind-a'
	@echo '  - Cluster B: export KUBECONFIG=${KUBECONFIG_PATH}-pe-kind-b'

.PHONY: clean
clean: kind ## Shutdown and clean up kind cluster(s) and containerlab topology.
	KUBECONFIG_PATH=$(KUBECONFIG_PATH) KIND=$(KIND) CLAB_TOPOLOGY=singlecluster/kind.clab.yml clab/clean.sh pe-kind
	KUBECONFIG_PATH=$(KUBECONFIG_PATH) KIND=$(KIND) CLAB_TOPOLOGY=multicluster/kind.clab.yml clab/clean.sh pe-kind-a pe-kind-b

.PHONY: load-on-kind
load-on-kind: ## Load the docker image into the kind cluster.
	KIND=$(KIND) bash -c 'source clab/common.sh && load_local_image_to_kind ${IMG} router'

.PHONY: load-on-multi-cluster
load-on-multi-cluster: ## Load the docker image into both kind clusters.
	KIND=$(KIND) bash -c 'export KIND_CLUSTER_NAME=pe-kind-a && source clab/common.sh && load_local_image_to_kind ${IMG} router-a'
	KIND=$(KIND) bash -c 'export KIND_CLUSTER_NAME=pe-kind-b && source clab/common.sh && load_local_image_to_kind ${IMG} router-b'

##@ Kind Node Image

.PHONY: kind-node-image-build
kind-node-image-build: ## Build custom kind node image with OVS
	cd hack/kind-node-image && KIND_NODE_VERSION=$(KIND_NODE_VERSION) ./build.sh

.PHONY: kind-node-image-push
kind-node-image-push: ## Push custom kind node image to quay.io
	cd hack/kind-node-image && ./push.sh

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
	cat website/content/docs/api-reference.md.template > website/content/docs/api-reference.md
	cat ./API-DOCS.md >> website/content/docs/api-reference.md

.PHONY: bumpversion
bumpversion:
	hack/release/pre_bump.sh
	hack/release/bumpversion.sh

.PHONY: cutrelease
cutrelease: bumpversion generate-all-in-one helm-docs api-docs bundle
	hack/release/release.sh

.PHONY: build-validator
build-validator: ginkgo ## Build Ginkgo test binary.
	CGO_ENABLED=0 $(GINKGO) build -tags=externaltests ./internal/hostnetwork
	mv internal/hostnetwork/hostnetwork.test $(VALIDATOR_PATH)

.PHONY: create-export-logs
create-export-logs:
	mkdir -p ${KIND_EXPORT_LOGS}

.PHONY: hugo-download
hugo-download:
	@if [ -x $(HUGO) ] && $(HUGO) version | grep -q '$(HUGO_VERSION)'; then :; \
	else \
		mkdir -p bin; \
		HUGO_ARCH=$$(go env GOOS)-$$(go env GOARCH); \
		curl -L https://github.com/gohugoio/hugo/releases/download/$(HUGO_VERSION)/hugo_extended_$(subst v,,$(HUGO_VERSION))_$${HUGO_ARCH}.tar.gz | tar -xz -C bin hugo; \
	fi

.PHONY: serve-website
serve-website: hugo-download api-docs
	$(HUGO) --source website server

.PHONY: build-website
build-website: hugo-download api-docs ## Build the website with API documentation
	$(HUGO) --source website --minify

.PHONY: publish-website
publish-website: ## Build and publish the website to gh-pages branch
	hack/publish-website.sh

.PHONY: demo-metallb-evpn
demo-metallb:
	examples/evpn/metallb/prepare.sh
	
.PHONY: demo-l2-evpn
demo-l2:
	examples/evpn/layer2/prepare.sh

.PHONY: demo-calico-evpn
demo-calico:
	examples/evpn/calico/prepare.sh

.PHONY: demo-kubevirt-evpn
demo-kubevirt:
	examples/evpn/kubevirt/prepare.sh

.PHONY: demo-metallb-passthrough
demo-metallb-passthrough:
	examples/passthrough/metallb/prepare.sh

.PHONY: demo-multi-cluster
demo-multi-cluster:
	examples/evpn/multi-cluster/prepare.sh
#
# Operator specifics, copied from a Makefile generated on a clean folder by operator-sdk, then modified.
#

CSV_VERSION = $(shell echo $(IMG_TAG) | sed 's/v//')
ifeq ($(IMG_TAG), main)
CSV_VERSION := 0.0.0
endif
ifeq ($(IMG_TAG), dev)
CSV_VERSION := 0.0.0
endif

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

IMAGE_TAG_BASE ?= $(IMG_REPO)/openperouter-operator
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(CSV_VERSION)
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(CSV_VERSION) $(BUNDLE_METADATA_OPTS)

USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

OPERATOR_SDK_VERSION ?= v1.41.1
OLM_VERSION ?= v0.32.0

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
	@if [ ! -x $(OPERATOR_SDK) ] || ! $(OPERATOR_SDK) version | grep -q $(OPERATOR_SDK_VERSION); then \
		set -e ;\
		mkdir -p $(dir $(OPERATOR_SDK)) ;\
		OS=$$(go env GOOS) && ARCH=$$(go env GOARCH) && \
		curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
		chmod +x $(OPERATOR_SDK) ;\
	fi

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	cd operator && $(OPERATOR_SDK) generate kustomize manifests --interactive=false -q
	cd operator/config/pods && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd operator/config/webhook/backend && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd operator && $(KUSTOMIZE) build config/default | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS) --extra-service-accounts "controller,perouter" --package openperouter-operator
	cd operator && $(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	cd operator && $(CONTAINER_ENGINE) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:v$(CSV_VERSION)

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
USE_HTTP ?= ""
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool $(CONTAINER_ENGINE) --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT) $(USE_HTTP)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)


deploy-operator-with-olm: export VERSION=dev
deploy-operator-with-olm: export CSV_VERSION=0.0.0
deploy-operator-with-olm: export KIND_WITH_REGISTRY=true
deploy-operator-with-olm: bundle kustomize kind clab-cluster load-on-kind deploy-olm build-and-push-bundle-images ## deploys the operator with OLM instead of manifests
	sed -i 's|image:.*|image: $(CATALOG_IMG)|' operator/config/olm-install/install-resources.yaml
	sed -i 's#openperouter-system#$(NAMESPACE)#g' operator/config/olm-install/install-resources.yaml
	$(KUSTOMIZE) build operator/config/olm-install | kubectl apply -f -
	VERSION=$(CSV_VERSION) NAMESPACE=$(NAMESPACE) hack/wait-for-csv.sh

deploy-olm: operator-sdk ## deploys OLM on the cluster
	@if $(KUBECTL) get deployment -n olm olm-operator > /dev/null 2>&1; then \
		echo "OLM already installed, skipping installation."; \
	else \
		echo "OLM not found, installing..."; \
		$(OPERATOR_SDK) olm install --version $(OLM_VERSION) --timeout 5m0s; \
		$(OPERATOR_SDK) olm status; \
	fi

build-and-push-bundle-images: bundle-build bundle-push catalog-build catalog-push

