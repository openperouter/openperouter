##@ QEMU E2E

QEMU_SSH_PORT ?= 2222
QEMU_WORKER_SSH_PORT ?= 2223
QEMU_K8S_PORT ?= 6443
QEMU_DIR := clab/qemu
QEMU_VM_IMAGE := $(QEMU_DIR)/vm/fedora-cloud.qcow2
QEMU_CLOUD_INIT_ISO := $(QEMU_DIR)/vm/cloud-init.iso

.PHONY: qemu-image
qemu-image: $(QEMU_VM_IMAGE) $(QEMU_CLOUD_INIT_ISO) ## Prepare the QEMU VM image with cloud-init

$(QEMU_VM_IMAGE):
	$(QEMU_DIR)/vm/prepare-vm-image.sh

$(QEMU_CLOUD_INIT_ISO):
	$(QEMU_DIR)/vm/prepare-vm-iso.sh

.PHONY: qemu-load-image
qemu-load-image: ## Load the container image into both QEMU VMs
	QEMU_SSH_PORT=$(QEMU_SSH_PORT) QEMU_WORKER_SSH_PORT=$(QEMU_WORKER_SSH_PORT) \
	  $(QEMU_DIR)/vm/load-image.sh $(IMG)

.PHONY: qemu-clab
qemu-clab: qemu-image kubectl ## Deploy containerlab topology and bootstrap k3s in both QEMU guests
	QEMU_SSH_PORT=$(QEMU_SSH_PORT) \
	QEMU_WORKER_SSH_PORT=$(QEMU_WORKER_SSH_PORT) \
	KUBECTL=$(KUBECTL) \
	QEMU_K8S_PORT=$(QEMU_K8S_PORT) \
	KUBECONFIG_PATH=$(KUBECONFIG_PATH) \
	  $(QEMU_DIR)/deploy-clab.sh

.PHONY: qemu-deploy
qemu-deploy: export KUSTOMIZE_LAYER=grout-qemu
qemu-deploy: qemu-clab ## Full QEMU environment deploy
	$(MAKE) qemu-load-image
	$(MAKE) deploy-controller

.PHONY: qemu-e2etests
qemu-e2etests: ginkgo kubectl create-export-logs ## Run QEMU e2e tests (both VMs must be running)
	$(GINKGO) -v $(GINKGO_ARGS) --timeout=1h \
	  ./e2etests/hw_emulation_suite -- \
	  --kubectl=$(KUBECTL) \
	  --inspect-path=$(CURDIR)/tools/inspect/inspect \
	  --reporterpath=$(KIND_EXPORT_LOGS) \
	  $(TEST_ARGS)

.PHONY: qemu-clean
qemu-clean: ## Tear down clab topology (stops both QEMU VMs)
	CLAB_TOPOLOGY=qemu/kind.clab.yml clab/clean.sh pe-kind

.PHONY: qemu-destroy
qemu-destroy: qemu-clean ## Fully destroy clab topology, VM disk images, logs, and SSH keys
	rm -f "$(QEMU_DIR)/vm/fedora-cloud.qcow2"
	rm -f "$(QEMU_DIR)/vm/cloud-init.iso"
	rm -f "$(QEMU_DIR)/vm/qemu-vm-key"
	rm -f "$(QEMU_DIR)/vm/qemu-vm-key.pub"
	rm -f "$(QEMU_DIR)/vm/overlay.qcow2" "$(QEMU_DIR)/vm/overlay-"*.qcow2
	rm -f "$(QEMU_DIR)/vm/serial.log" "$(QEMU_DIR)/vm/serial-"*.log

.PHONY: qemu-ssh qemu-ssh-worker
qemu-ssh-worker: QEMU_SSH_TARGET_PORT = $(QEMU_WORKER_SSH_PORT)
qemu-ssh-worker: qemu-ssh

QEMU_SSH_TARGET_PORT = $(QEMU_SSH_PORT)
qemu-ssh:
	ssh -p "$(QEMU_SSH_TARGET_PORT)" \
	    -i clab/qemu/vm/qemu-vm-key \
	    -o StrictHostKeyChecking=no \
	    -o UserKnownHostsFile=/dev/null \
	    openperouter@127.0.0.1
