#!/usr/bin/env bash
# The gateway's network, at every boot, as the nodes already have
# (chromebox-node.service, setup.sh section 7). Before this, a reboot of the
# gateway lost its tunnels to the nodes, the NAT that takes VM traffic out and
# the firewall rules, and nothing worked until someone ran overlay.sh and
# firewall.sh from a laptop. Runs on the gateway as ec2-user (uses sudo). Safe
# to re-run.
#
#   scripts/aws/nodes.sh ssh gateway 'bash -s' < scripts/host/gateway-setup.sh
#
# /etc/chromebox/tunnels lists "node private-ip" per line; overlay.sh rewrites
# it whenever nodes change. The first run here takes it from the tunnels that
# exist. Keep gateway.nft in step with GATEWAY_RULES in firewall.sh.
set -euo pipefail
sudo mkdir -p /etc/chromebox

if [ ! -s /etc/chromebox/tunnels ]; then
  ip -d -o link show type gre | sed -n 's/^[0-9]*: gre-\([^@:]*\)[@:].* remote \([0-9.]*\) .*/\1 \2/p' | sudo tee /etc/chromebox/tunnels >/dev/null
fi
echo "tunnels: $(tr '\n' ' ' < /etc/chromebox/tunnels)"

sudo tee /etc/chromebox/gateway.nft >/dev/null <<'NFT'
table inet baton {
  set blocked { type ipv4_addr; flags interval;
    elements = { 169.254.0.0/16, 172.16.0.0/12, 10.0.0.0/8, 192.168.0.0/16, 100.64.0.0/10, 127.0.0.0/8 } }
  chain forward { type filter hook forward priority 0; policy accept;
    ct state established,related accept
    ip saddr 10.200.0.0/16 ip daddr @blocked counter drop
    ip saddr 10.200.0.0/16 tcp dport { 25, 465, 587 } counter drop
  }
  chain input { type filter hook input priority 0; policy accept;
    ip saddr 10.200.0.0/16 ct state established,related accept
    ip saddr 10.200.0.0/16 tcp dport 3129 ct status dnat accept   # a proxied VM's traffic, redirected to the egress listener
    ip saddr 10.200.0.0/16 counter drop
  }
}
NFT

sudo tee /usr/local/bin/chromebox-gateway-boot >/dev/null <<'SH'
#!/bin/sh
set -eu
modprobe ip_gre
sysctl -q -p /etc/sysctl.d/90-chromebox.conf || true
WAN=$(ip -o route show default | awk '{print $5}' | head -1)
GW_IP=$(ip -4 -o addr show dev "$WAN" | awk '{print $4}' | cut -d/ -f1 | head -1)
while read -r name ip; do
  [ -n "$name" ] || continue
  ip link show "gre-$name" >/dev/null 2>&1 || ip tunnel add "gre-$name" mode gre local "$GW_IP" remote "$ip" ttl 255
  ip link set "gre-$name" up
done < /etc/chromebox/tunnels
# The firewall, then NAT for the VMs' traffic to the internet.
nft delete table inet baton 2>/dev/null || true
nft -f /etc/chromebox/gateway.nft
nft delete table ip baton-nat 2>/dev/null || true
nft add table ip baton-nat
nft add chain ip baton-nat postrouting '{ type nat hook postrouting priority srcnat; }'
nft add rule ip baton-nat postrouting oifname "$WAN" ip saddr 10.200.0.0/16 masquerade
echo "gateway network up: $(wc -l < /etc/chromebox/tunnels) tunnels from $GW_IP, NAT out $WAN, firewall loaded"
SH
sudo chmod 755 /usr/local/bin/chromebox-gateway-boot

sudo tee /etc/systemd/system/chromebox-gateway.service >/dev/null <<'U'
[Unit]
Description=chromebox gateway network (tunnels to the nodes, NAT, firewall)
After=network-online.target
Wants=network-online.target
Before=chromebox-orchestrator.service chromebox-proxy.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/chromebox-gateway-boot
[Install]
WantedBy=multi-user.target
U
# The orchestrator puts each VM's route on these tunnels when it starts.
for u in chromebox-orchestrator chromebox-proxy; do
  f=/etc/systemd/system/$u.service
  if [ -f "$f" ] && ! grep -q chromebox-gateway.service "$f"; then
    sudo sed -i 's/^After=network-online.target$/After=network-online.target chromebox-gateway.service\nWants=chromebox-gateway.service/' "$f"
  fi
done
sudo systemctl daemon-reload
sudo systemctl enable chromebox-gateway.service >/dev/null 2>&1
sudo systemctl restart chromebox-gateway.service
systemctl is-active chromebox-gateway.service >/dev/null && sudo journalctl -u chromebox-gateway --no-pager -n 1 -o cat
