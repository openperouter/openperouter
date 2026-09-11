##@ QEMU E2E

QEMU_SSH_PORT ?= 2222
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
qemu-load-image: ## Load the container image into the QEMU VM's k3s
	$(QEMU_DIR)/vm/load-image.sh $(IMG)

.PHONY: qemu-clab
qemu-clab: qemu-image kubectl ## Deploy containerlab topology and bootstrap k3s in the QEMU guest
	KUBECONFIG_PATH=$(KUBECONFIG_PATH) \
	  $(QEMU_DIR)/deploy-clab.sh

.PHONY: qemu-deploy
qemu-deploy: qemu-clab qemu-load-image deploy-controller ## Full QEMU environment deploy

.PHONY: qemu-e2etests
qemu-e2etests: ginkgo kubectl create-export-logs ## Run QEMU e2e tests (VM must be running)
	$(GINKGO) -v $(GINKGO_ARGS) --timeout=1h \
	  ./e2etests/qemu_suite -- \
	  --kubectl=$(KUBECTL) \
	  --reporterpath=$(KIND_EXPORT_LOGS) \
	  $(TEST_ARGS)

.PHONY: qemu-collect-logs
qemu-collect-logs: create-export-logs ## Collect QEMU VM and FRR logs
	$(QEMU_DIR)/vm/collect-logs.sh

.PHONY: qemu-clean
qemu-clean: ## Tear down clab topology (stops the QEMU VM)
	CLAB_TOPOLOGY=qemu/kind.clab.yml clab/clean.sh pe-kind

.PHONY: qemu-destroy
qemu-destroy: qemu-clean ## Fully destroy clab topology, VM disk image, and SSH keys
	rm -f "$(QEMU_DIR)/vm/fedora-cloud.qcow2"
	rm -f "$(QEMU_DIR)/vm/cloud-init.iso"
	rm -f "$(QEMU_DIR)/vm/qemu-vm-key"
	rm -f "$(QEMU_DIR)/vm/qemu-vm-key.pub"
	rm -f "$(QEMU_DIR)/vm/serial.log"

.PHONY: qemu-ssh
qemu-ssh:
	ssh -p "$(QEMU_SSH_PORT)" \
	    -i clab/qemu/vm/qemu-vm-key \
	    -o StrictHostKeyChecking=no \
	    -o UserKnownHostsFile=/dev/null \
	    openperouter@127.0.0.1
