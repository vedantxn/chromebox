#!/usr/bin/env bash
# Network settings a chromebox node needs. Runs on the node. Safe to re-run.
#   ip_forward: a node routes packets between the gateway tunnel and the VMs' TAPs.
#   rp_filter off: strict reverse-path filtering drops packets that arrive on one
#     interface when the route back to their source points out another, which
#     is exactly what happens to traffic that arrives through the tunnel.
set -euo pipefail
sudo tee /etc/sysctl.d/90-chromebox.conf >/dev/null <<'EOC'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
EOC
sudo sysctl -q --system
echo "ok: forwarding=$(sysctl -n net.ipv4.ip_forward) rp_filter(all)=$(sysctl -n net.ipv4.conf.all.rp_filter)"
