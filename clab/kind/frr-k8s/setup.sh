#!/bin/bash

# TO REMOVE ONCE fixed in frr-k8s
kubectl apply -f https://raw.githubusercontent.com/metallb/frr-k8s/main/config/all-in-one/frr-k8s.yaml
kubectl apply -f kind/frr-k8s/client.yaml
sleep 2s
kubectl -n frr-k8s-system wait --for=condition=Ready --all pods --timeout 300s

start=$(date +%s)

while true; do
    kubectl apply -f kind/frr-k8s/test-config.yaml && break
    now=$(date +%s)
    elapsed=$((now - start))
    if [ $elapsed -ge 60 ]; then
        echo "Timeout reached"
        break
    fi
    sleep 1
done
