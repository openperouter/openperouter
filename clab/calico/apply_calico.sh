#!/bin/bash

source "$(dirname $(readlink -f $0))/../common.sh"

timeout=600
start_time=$(date +%s)

check_timeout() {
    local current_time=$(date +%s)
    local elapsed=$((current_time - start_time))
    if [ "$elapsed" -ge "$timeout" ]; then
        echo "Timeout reached after $timeout seconds."
        exit 1
    fi
}

# Loop until all commands succeed or timeout
while true; do
    sleep 5
    check_timeout
    echo "Apply calico."
    if ! ${KIND_COMMAND} get kubeconfig --name pe-kind > kubeconfig; then
	    continue
    fi
    export KUBECONFIG=./kubeconfig
    if ! kubectl apply --server-side=true -f tigera-operator.yaml; then
	    continue
    fi

    if ! kubectl apply --server-side=true -f calico-config.yaml; then
	    continue
    fi

    echo "Apply calico succeeded."
    exit 0

done

