#!/bin/bash
set -e

echo "========================================"
echo "Starting leafgcp with VPN to GCP"
echo "========================================"

# Start FRR
echo "[1/4] Starting FRR..."
/etc/init.d/frr start
sleep 2

# Enable IP forwarding
echo "[2/4] Enabling IP forwarding..."
echo 1 > /proc/sys/net/ipv4/ip_forward
echo 1 > /proc/sys/net/ipv6/conf/all/forwarding

# Configure strongSwan VPN
echo "[3/4] Configuring strongSwan VPN..."
VPN_REMOTE_IP=${VPN_REMOTE_IP:-34.16.52.245}
VPN_LOCAL_IP=${VPN_LOCAL_IP:-79.116.24.173}
VPN_SECRET=${VPN_SECRET:-RdRTEpoYc/2E44SSIv3bfwGsN0PkNuBP}

cat > /etc/swanctl/conf.d/gcp.conf <<EOF
connections {
    gcp-tunnel {
        version = 2
        local_addrs = %defaultroute
        remote_addrs = ${VPN_REMOTE_IP}

        local {
            auth = psk
            id = ${VPN_LOCAL_IP}
        }

        remote {
            auth = psk
            id = ${VPN_REMOTE_IP}
        }

        children {
            gcp-tunnel {
                local_ts = 10.250.1.0/24
                remote_ts = 10.0.200.0/24
                esp_proposals = aes256-sha256-modp2048
                dpd_action = restart
                start_action = start
                close_action = restart
            }
        }

        proposals = aes256-sha256-modp2048
        dpd_delay = 10s
        dpd_timeout = 30s
        keyingtries = 0
        unique = never
    }
}

secrets {
    ike-gcp {
        id-1 = ${VPN_LOCAL_IP}
        id-2 = ${VPN_REMOTE_IP}
        secret = "${VPN_SECRET}"
    }
}
EOF

echo "  VPN Configuration:"
echo "    Local:  ${VPN_LOCAL_IP}"
echo "    Remote: ${VPN_REMOTE_IP}"
echo "    Traffic Selectors:"
echo "      Local:  10.250.1.0/24 (containerlab underlay)"
echo "      Remote: 10.0.200.0/24 (GCP VTEPs)"

# Start strongSwan
echo "[4/4] Starting strongSwan..."
ipsec start --nofork &
CHARON_PID=$!

# Wait for charon to be ready
sleep 3

# Load configurations
swanctl --load-all

echo ""
echo "========================================"
echo "leafgcp started successfully!"
echo "========================================"
echo ""
echo "BGP Status:"
vtysh -c "show bgp summary" || true

echo ""
echo "VPN Status:"
swanctl --list-sas || true

echo ""
echo "Monitoring logs (Ctrl+C to stop)..."
echo "========================================"

# Keep container running and show logs
# Use sleep infinity to keep container running even if log files don't exist
tail -f /var/log/frr/frr.log 2>/dev/null &
sleep infinity
