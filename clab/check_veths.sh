#!/bin/bash

set -u

source common.sh

# Override set -e from common.sh: the monitoring loop must survive
# individual command failures and retry on the next iteration.
set +e

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
  CONTAINER_SIDE_IPS=$4
  CONTAINER_SIDE_IP_ARRAY=()
  IFS=',' read -ra CONTAINER_SIDE_IP_ARRAY <<< "$CONTAINER_SIDE_IPS"

  TEMP_PEER_NAME="${PEER_NAME}_temp"
  if ! veth_exists "$VETH_NAME"; then
    echo "Veth $VETH_NAME not there, recreating"

    # Clean up stale interfaces from previous failed attempts
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link delete "$PEER_NAME" 2>/dev/null || true
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link delete "$TEMP_PEER_NAME" 2>/dev/null || true
    ip link delete "$TEMP_PEER_NAME" 2>/dev/null || true

    ip link add "$VETH_NAME" type veth peer name "$TEMP_PEER_NAME"
    echo "Veth $VETH_NAME not there, recreated"
    pid=$("$CONTAINER_ENGINE_CLI" inspect -f '{{.State.Pid}}' "$CONTAINER_NAME")
    ip link set "$TEMP_PEER_NAME" netns "$pid"
    ip link set "$VETH_NAME" up

    ip link set "$VETH_NAME" master leafkind-switch

    MAC_ADDR="02:ed:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]"
    ip link set "$VETH_NAME" address "$MAC_ADDR"
    echo "Veth $VETH_NAME setting ip"
    for CONTAINER_SIDE_IP in "${CONTAINER_SIDE_IP_ARRAY[@]}"; do
      "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip address add "$CONTAINER_SIDE_IP" dev "$TEMP_PEER_NAME"
    done
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link set "$TEMP_PEER_NAME" name "$PEER_NAME"
    MAC_ADDR="02:ed:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]:$[RANDOM%10]$[RANDOM%10]"
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link set "$PEER_NAME" address "$MAC_ADDR"
    "$CONTAINER_ENGINE_CLI" exec "$CONTAINER_NAME" ip link set "$PEER_NAME" up
  fi
}

nodes=("$@")

node_parts=()
while true; do

for node in "${nodes[@]}"; do

    IFS=';' read -ra node_parts <<< "$node"
    veth_name="${node_parts[0]}"
    peer_name="${node_parts[1]}"
    container_name="${node_parts[2]}"
    container_side_ips="${node_parts[3]}"

    if ! container_exists "$container_name"; then
      echo "Container $container_name does not exist. Exiting."
      exit 1
    fi

    ensure_veth $veth_name $peer_name $container_name $container_side_ips
done
sleep 5s
done
