#!/bin/bash

set -u
set -x

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

  TEMP_PEER_NAME="${PEER_NAME}_temp"

  if ! veth_exists "$VETH_NAME"; then
    echo "Veth $VETH_NAME not there, recreating"
    ip link add "$VETH_NAME" type veth peer name "$TEMP_PEER_NAME"
    echo "Veth $VETH_NAME not there, recreated"
    pid=$("$CONTAINER_ENGINE_CLI" inspect -f '{{.State.Pid}}' "$CONTAINER_NAME")
    ip link set "$TEMP_PEER_NAME" netns "$pid"
    ip link set "$VETH_NAME" up

    ip link set "$VETH_NAME" master leafkind-switch

    MAC_ADDR="02:ed:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]"
    ip link set "$VETH_NAME" address "$MAC_ADDR"
    echo "Veth $VETH_NAME setting ip"
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip address add $CONTAINER_SIDE_IP dev "$TEMP_PEER_NAME"
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link set "$TEMP_PEER_NAME" up
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link set "$TEMP_PEER_NAME" name "$PEER_NAME"
    MAC_ADDR="02:ed:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]"

    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link set "$PEER_NAME" address "$MAC_ADDR"
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
