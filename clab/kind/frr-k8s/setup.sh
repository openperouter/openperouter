#!/bin/bash

kubectl apply -k $(dirname ${BASH_SOURCE[0]})
sleep 2s
kubectl -n frr-k8s-system wait --for=condition=Ready --all pods --timeout 300s

