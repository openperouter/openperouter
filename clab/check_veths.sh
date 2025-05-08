#!/bin/bash

set -u

source common.sh

function veth_exists {
    ip link show "$1" &> /dev/null
    return $?
}

function container_exists {
    ${CONTAINER_ENGINE_CLI} ps -a --format '{{.Names}}' | grep -w "$1" &> /dev/null
    return $?
}


function ensure_veth {
  VETH_NAME=$1
  PEER_NAME=$2
  CONTAINER_NAME=$3
  CONTAINER_SIDE_IP=$4

  if ! veth_exists "$VETH_NAME"; then
    echo "Veth $VETH_NAME not there, recreating"
    ip link add "$VETH_NAME" type veth peer name "$PEER_NAME"
    echo "Veth $VETH_NAME not there, recreated"
    pid=$("$CONTAINER_ENGINE_CLI" inspect -f '{{.State.Pid}}' "$CONTAINER_NAME")
    ip link set "$PEER_NAME" netns "$pid"
    ip link set "$VETH_NAME" up

    ip link set "$VETH_NAME" master leafkind-switch
    echo "Veth $VETH_NAME setting ip"
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip address add $CONTAINER_SIDE_IP dev "$PEER_NAME"
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link set "$PEER_NAME" up
  fi
}

nodes=("$@")

node_parts=()
while true; do

for node in "${nodes[@]}"; do

    IFS=':' read -ra node_parts <<< "$node"
    veth_name="${node_parts[0]}"
    peer_name="${node_parts[1]}"
    container_name="${node_parts[2]}"
    container_side_ip="${node_parts[3]}"

    if ! container_exists "$container_name"; then
      echo "Container $container_name does not exist. Exiting."
      exit 1
    fi

    ensure_veth $veth_name $peer_name $container_name $container_side_ip
done
sleep 5s
done
