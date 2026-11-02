#!/usr/bin/env bash
# Add isolated libvirt networks and static node interfaces after installation.
#
# Required configuration (normally config_$USER.sh):

POST_INSTALL_EXTRA_NETWORK_NAMES="toswitch1 toswitch2"
POST_INSTALL_TOSWITCH1_GATEWAY_V4="192.168.11.254/24"
POST_INSTALL_TOSWITCH1_GATEWAY_V6="2001:db8:11::1/64"
POST_INSTALL_TOSWITCH1_NODE_IPS_V4="192.168.11.100/24 192.168.11.101/24 192.168.11.102/24 192.168.11.103/24 192.168.11.104/24"
POST_INSTALL_TOSWITCH1_NODE_IPS_V6="2001:db8:11::100/64 2001:db8:11::101/64 2001:db8:11::102/64 2001:db8:11::103/64 2001:db8:11::104/64"
POST_INSTALL_TOSWITCH2_GATEWAY_V4="192.168.12.1/24"
POST_INSTALL_TOSWITCH2_GATEWAY_V6="2001:db8:12::1/64"
POST_INSTALL_TOSWITCH2_NODE_IPS_V4="192.168.12.100/24 192.168.12.101/24 192.168.12.102/24 192.168.12.103/24 192.168.12.104/24"
POST_INSTALL_TOSWITCH2_NODE_IPS_V6="2001:db8:12::100/64 2001:db8:12::101/64 2001:db8:12::102/64 2001:db8:12::103/64 2001:db8:12::104/64"

# Node IP order: masters first, then workers. Repeat variables per network.
set -euo pipefail

output_dir="${1:-/tmp/dev-scripts-setup-extra-networks}"
mkdir -p "$output_dir"
exec > >(tee "$output_dir/run.log") 2>&1

config_file="${CONFIG:-$PWD/config_${USER}.sh}"
if [[ ! -r "$config_file" ]]; then
  echo "Missing config: $config_file"
  exit 2
fi
# shellcheck source=/dev/null
source "$config_file"

: "${CLUSTER_NAME:?CLUSTER_NAME must be set}"
: "${NUM_MASTERS:?NUM_MASTERS must be set}"
: "${POST_INSTALL_EXTRA_NETWORK_NAMES:?Set POST_INSTALL_EXTRA_NETWORK_NAMES; do not set EXTRA_NETWORK_NAMES during install}"
read -r -a extra_networks <<<"$POST_INSTALL_EXTRA_NETWORK_NAMES"
KUBECONFIG="${KUBECONFIG:-$PWD/ocp/$CLUSTER_NAME/auth/kubeconfig}"
export KUBECONFIG
BAREMETAL_NETWORK_NAME="${BAREMETAL_NETWORK_NAME:-${CLUSTER_NAME}bm}"
POST_INSTALL_VM_SHUTDOWN_TIMEOUT="${POST_INSTALL_VM_SHUTDOWN_TIMEOUT:-600}"
POST_INSTALL_VM_START_TIMEOUT="${POST_INSTALL_VM_START_TIMEOUT:-1200}"
POST_INSTALL_CLUSTER_TIMEOUT="${POST_INSTALL_CLUSTER_TIMEOUT:-1800}"
expected_nodes=$((NUM_MASTERS + ${NUM_WORKERS:-0}))
declare -A domains_need_restart=()
cluster_domains=()
for node_index in $(seq 0 $((NUM_MASTERS - 1))); do
  cluster_domains+=("${CLUSTER_NAME}_master_${node_index}")
done
if (( ${NUM_WORKERS:-0} > 0 )); then
  for node_index in $(seq 0 $((NUM_WORKERS - 1))); do
    cluster_domains+=("${CLUSTER_NAME}_worker_${node_index}")
  done
fi

if ! oc get nodes >/dev/null; then
  echo "Cluster unavailable through KUBECONFIG=$KUBECONFIG"
  exit 2
fi

prefix_name() {
  tr '[:lower:]-' '[:upper:]_' <<<"$1"
}

v4_netmask() {
  local prefix="$1" mask= value i
  (( prefix >= 0 && prefix <= 32 )) || return 1
  value=0
  for ((i = 0; i < 4; i++)); do
    if (( prefix >= 8 )); then mask+="255"; prefix=$((prefix - 8))
    elif (( prefix == 0 )); then mask+="0"
    else value=$((256 - 2 ** (8 - prefix))); mask+="$value"; prefix=0
    fi
    (( i < 3 )) && mask+='.'
  done
  printf '%s\n' "$mask"
}

network_is_active() {
  local network="$1"
  sudo virsh net-list --name | awk -v network="$network" \
    '$0 == network { found=1 } END { exit !found }'
}

define_network() {
  local network="$1" gateway_v4="$2" gateway_v6="$3" xml="$output_dir/$network.xml"
  local ip prefix mask
  {
    printf '<network>\n  <name>%s</name>\n  <bridge name="%s" stp="on" delay="0"/>\n' "$network" "$network"
    printf '  <forward mode="nat">\n    <nat>\n      <port start="1024" end="65535"/>\n    </nat>\n  </forward>\n'
    if [[ -n "$gateway_v4" ]]; then
      ip="${gateway_v4%/*}"; prefix="${gateway_v4#*/}"; mask="$(v4_netmask "$prefix")"
      printf '  <ip address="%s" netmask="%s"/>\n' "$ip" "$mask"
    fi
    if [[ -n "$gateway_v6" ]]; then
      ip="${gateway_v6%/*}"; prefix="${gateway_v6#*/}"
      printf '  <ip family="ipv6" address="%s" prefix="%s"/>\n' "$ip" "$prefix"
    fi
    printf '</network>\n'
  } >"$xml"

  if sudo virsh net-info "$network" >/dev/null 2>&1; then
    if network_is_active "$network"; then
      sudo virsh net-destroy "$network"
    fi
    sudo virsh net-undefine "$network"
    for domain in "${cluster_domains[@]}"; do
      domains_need_restart["$domain"]=1
    done
  fi
  sudo virsh net-define "$xml"
  sudo virsh net-autostart "$network"
  if ! network_is_active "$network"; then
    sudo virsh net-start "$network" || network_is_active "$network"
  fi
}

node_ip_for_domain() {
  local domain="$1" host_mac
  host_mac="$(sudo virsh domiflist "$domain" --inactive | awk -v network="$BAREMETAL_NETWORK_NAME" '$3 == network && !found { print $5; found=1 }')"
  sudo virsh net-dhcp-leases "$BAREMETAL_NETWORK_NAME" | awk -v mac="$host_mac" \
    '$3 == mac && $4 == "ipv4" && !found { sub(/\/.*/, "", $5); print $5; found=1 }'
}

wait_for_cluster() {
  local deadline=$((SECONDS + POST_INSTALL_CLUSTER_TIMEOUT)) node_count
  echo "Waiting for the API and all $expected_nodes nodes to be Ready"
  while (( SECONDS < deadline )); do
    node_count="$(oc get nodes -o name 2>/dev/null | wc -l || true)"
    if (( node_count >= expected_nodes )) && \
       oc get --raw=/readyz >/dev/null 2>&1 && \
       oc wait --for=condition=Ready nodes --all --timeout=10s >/dev/null 2>&1; then
      echo "API is reachable and all nodes are Ready"
      return 0
    fi
    sleep 10
  done
  echo "Timed out waiting for the API and all $expected_nodes nodes to be Ready"
  return 1
}

attach_node_interface() {
  local network="$1" net_index="$2" role="$3" node_index="$4"
  local domain="${CLUSTER_NAME}_${role}_${node_index}" mac mac_node_index="$node_index"

  mac="$(sudo virsh domiflist "$domain" --inactive | awk -v network="$network" '$3 == network && !found { print $5; found=1 }')"
  if [[ -z "$mac" ]]; then
    [[ "$role" == worker ]] && mac_node_index=$((node_index + 128))
    printf -v mac '52:54:00:ee:%02x:%02x' "$net_index" "$mac_node_index"
    sudo virsh attach-interface "$domain" network "$network" --model virtio --mac "$mac" --config
    domains_need_restart["$domain"]=1
  fi

  if ! sudo virsh domiflist "$domain" | awk -v network="$network" '$3 == network { found=1 } END { exit !found }'; then
    domains_need_restart["$domain"]=1
  fi
  if [[ "$(sudo virsh domstate "$domain")" != "running" ]]; then
    domains_need_restart["$domain"]=1
  fi
}

request_domain_shutdown() {
  local domain="$1"
  [[ -n "${domains_need_restart[$domain]:-}" ]] || return 0
  echo "Requesting shutdown of $domain"
  if [[ "$(sudo virsh domstate "$domain")" == "running" ]]; then
    sudo virsh shutdown "$domain"
  fi
}

wait_for_domain_shutdown() {
  local domain="$1" deadline=$((SECONDS + POST_INSTALL_VM_SHUTDOWN_TIMEOUT))
  [[ -n "${domains_need_restart[$domain]:-}" ]] || return 0
  until [[ "$(sudo virsh domstate "$domain")" == "shut off" ]]; do
    if (( SECONDS >= deadline )); then
      echo "Timed out waiting for $domain to shut down; refusing to force it off"
      return 1
    fi
    sleep 5
  done
  echo "$domain is shut off"
}

start_domain() {
  local domain="$1"
  [[ -n "${domains_need_restart[$domain]:-}" ]] || return 0
  echo "Starting $domain"
  sudo virsh start "$domain"
}

wait_for_domain() {
  local domain="$1" deadline=$((SECONDS + POST_INSTALL_VM_START_TIMEOUT)) node_ip
  [[ -n "${domains_need_restart[$domain]:-}" ]] || return 0
  while (( SECONDS < deadline )); do
    node_ip="$(node_ip_for_domain "$domain")"
    if [[ -n "$node_ip" ]] && ssh -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=no \
      -o UserKnownHostsFile=/dev/null "core@$node_ip" true >/dev/null 2>&1; then
      echo "$domain is running and reachable at $node_ip"
      return 0
    fi
    sleep 5
  done
  echo "Timed out waiting for $domain to become reachable"
  return 1
}

configure_node() {
  # iface is the libvirt network name, which is also the target interface name inside the VM.
  local iface="$1" role="$2" node_index="$3" ip4="$4" ip6="$5"
  local domain="${CLUSTER_NAME}_${role}_${node_index}"
  local node_ip mac
  node_ip="$(node_ip_for_domain "$domain")"
  [[ -n "$node_ip" ]] || { echo "No IPv4 lease for $domain on $BAREMETAL_NETWORK_NAME"; return 1; }

  mac="$(sudo virsh domiflist "$domain" | awk -v iface="$iface" '$3 == iface && !found { print $5; found=1 }')"
  [[ -n "$mac" ]] || { echo "No $iface interface found on $domain after restart"; return 1; }

  ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "core@$node_ip" \
    "echo 'SUBSYSTEM==\"net\", ATTR{address}==\"${mac}\", NAME=\"${iface}\"' \
       | sudo tee /etc/udev/rules.d/70-${iface}.rules
     sudo udevadm control --reload-rules
     current=\$(sudo ip -br link ls | awk '/${mac}/ {print \$1}')
     sudo ip link set dev \${current} down
     sudo udevadm trigger --subsystem-match=net --action=add"

  ssh -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "core@$node_ip" \
    "sudo nmcli connection delete '${iface}' >/dev/null 2>&1 || true
     sudo nmcli connection add type ethernet ifname '${iface}' con-name '${iface}' \
       ipv4.method '$([[ -n "$ip4" ]] && echo manual || echo disabled)' \
       ipv4.addresses '${ip4}' ipv4.never-default yes \
       ipv6.method '$([[ -n "$ip6" ]] && echo manual || echo disabled)' \
       ipv6.addresses '${ip6}' ipv6.never-default yes
     sudo nmcli connection up '${iface}'
     ip -brief address show dev '${iface}'"
}

# Define every network and add every persistent interface before restarting any VM.
network_index=0
for network in "${extra_networks[@]}"; do
  variable_prefix="$(prefix_name "$network")"
  gateway_v4_var="POST_INSTALL_${variable_prefix}_GATEWAY_V4"
  gateway_v6_var="POST_INSTALL_${variable_prefix}_GATEWAY_V6"
  ips_v4_var="POST_INSTALL_${variable_prefix}_NODE_IPS_V4"
  ips_v6_var="POST_INSTALL_${variable_prefix}_NODE_IPS_V6"
  gateway_v4="${!gateway_v4_var:-}"
  gateway_v6="${!gateway_v6_var:-}"
  read -r -a ips_v4 <<<"${!ips_v4_var:-}"
  read -r -a ips_v6 <<<"${!ips_v6_var:-}"
  [[ -n "$gateway_v4$gateway_v6" ]] || { echo "$network needs gateway IPv4 or IPv6"; exit 2; }

  define_network "$network" "$gateway_v4" "$gateway_v6"
  for node_index in $(seq 0 $((NUM_MASTERS - 1))); do
    attach_node_interface "$network" "$network_index" master "$node_index"
  done
  if (( ${NUM_WORKERS:-0} > 0 )); then
    for node_index in $(seq 0 $((NUM_WORKERS - 1))); do
      attach_node_interface "$network" "$network_index" worker "$node_index"
    done
  fi
  network_index=$((network_index + 1))
done

# Restart affected VMs as a batch, then require every VM and the cluster to recover.
for domain in "${cluster_domains[@]}"; do
  request_domain_shutdown "$domain"
done
for domain in "${cluster_domains[@]}"; do
  wait_for_domain_shutdown "$domain"
done
for domain in "${cluster_domains[@]}"; do
  start_domain "$domain"
done
for domain in "${cluster_domains[@]}"; do
  wait_for_domain "$domain"
done
wait_for_cluster

# Configure addresses only after every VM and the Kubernetes API are available.
for network in "${extra_networks[@]}"; do
  variable_prefix="$(prefix_name "$network")"
  ips_v4_var="POST_INSTALL_${variable_prefix}_NODE_IPS_V4"
  ips_v6_var="POST_INSTALL_${variable_prefix}_NODE_IPS_V6"
  read -r -a ips_v4 <<<"${!ips_v4_var:-}"
  read -r -a ips_v6 <<<"${!ips_v6_var:-}"
  node_position=0
  for node_index in $(seq 0 $((NUM_MASTERS - 1))); do
    configure_node "$network" master "$node_index" "${ips_v4[$node_position]:-}" "${ips_v6[$node_position]:-}"
    node_position=$((node_position + 1))
  done
  if (( ${NUM_WORKERS:-0} > 0 )); then
    for node_index in $(seq 0 $((NUM_WORKERS - 1))); do
      configure_node "$network" worker "$node_index" "${ips_v4[$node_position]:-}" "${ips_v6[$node_position]:-}"
      node_position=$((node_position + 1))
    done
  fi
done
wait_for_cluster

echo "Done. Output: $output_dir"
