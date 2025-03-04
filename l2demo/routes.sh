#!/bin/bash
# add local gw
kubectl get pods -n openperouter-system -l app=router -o name | xargs -I {} kubectl -n openperouter-system exec {} -- ip address add 192.170.1.0/24 dev br110
kubectl get pods -n openperouter-system -l app=router -o name | xargs -I {} kubectl -n openperouter-system exec {} -- ip address add 192.171.1.0/24 dev br210

# remove default route via eth0
kubectl get pods -o name | xargs -I {} kubectl exec {} -- ip route del default dev eth0
