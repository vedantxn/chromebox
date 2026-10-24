#!/usr/bin/env bash
# Build the network overlay between the gateway and the VM hosts. Runs on the
# dev machine and configures every running chromebox instance over ssh. The
# settings do not survive a reboot, so re-run it after starting the instances.
# Safe to re-run: it only adds what is missing.
#
#   gateway:  one GRE tunnel per node (gre-<node>), forwarding, NAT for the VMs'
#             internet traffic, and the CDP proxy service. The route for each VM
#             (10.200.n.0/30 dev gre-<node>) is added when the VM starts and
#             moved when it migrates.
#   nodes:    a GRE tunnel back to the gateway (gre-gw), and policy routing that
#             sends everything a VM originates (and that is not for another VM)
#             into that tunnel, so all VM traffic leaves through the gateway.
#   all:      /var/lib/chromebox/node.json, naming the node and its gateway.
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh

priv() { # private IP of a running instance by short name
  aws ec2 describe-instances --filters "Name=tag:Name,Values=chromebox-$1" Name=instance-state-name,Values=running \
    --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text
}
names=$(aws ec2 describe-instances --filters Name=tag:Project,Values=chromebox Name=instance-state-name,Values=running \
  --query 'Reservations[].Instances[].Tags[?Key==`Name`]|[].Value' --output text | tr '\t' '\n' | sed 's/^chromebox-//' | sort)
gw_ip=$(priv gateway)
[[ $gw_ip != None ]] || { echo "no running gateway instance (scripts/aws/launch-node.sh gateway t3.small)"; exit 1; }
nodes=$(grep '^node-' <<<"$names" || true)

echo "== gateway ($gw_ip)"
# A GRE tunnel is stateless, and a stopped instance keeps its private address,
# so the gateway gets a tunnel to every node, running or not: a node the
# autoscaler starts finds its tunnel ready.
tunnels=""
all_nodes=$(aws ec2 describe-instances --filters Name=tag:Project,Values=chromebox "Name=instance-state-name,Values=running,stopped,stopping,pending" \
  --query 'Reservations[].Instances[].[Tags[?Key==`Name`]|[0].Value,PrivateIpAddress]' --output text | sed 's/^chromebox-//' | grep '^node-' | sort || true)
while read -r name ip; do [ -n "$name" ] && tunnels+="$name $ip"$'\n'; done <<<"$all_nodes"
scripts/aws/nodes.sh ssh gateway "GW_IP=$gw_ip bash -s" <<EOS
set -euo pipefail
sudo modprobe ip_gre
sudo dnf install -y -q iptables-nft >/dev/null
printf 'net.ipv4.ip_forward = 1\nnet.ipv4.conf.all.rp_filter = 0\nnet.ipv4.conf.default.rp_filter = 0\n' | sudo tee /etc/sysctl.d/90-chromebox.conf >/dev/null
sudo sysctl -q -p /etc/sysctl.d/90-chromebox.conf
while read -r name ip; do
  [ -n "\$name" ] || continue
  ip link show "gre-\$name" >/dev/null 2>&1 || sudo ip tunnel add "gre-\$name" mode gre local "\$GW_IP" remote "\$ip" ttl 255
  sudo ip link set "gre-\$name" up
  echo "tunnel gre-\$name -> \$ip"
done <<'T'
$tunnels
T
wan=\$(ip -o route show default | awk '{print \$5}' | head -1)
sudo iptables -t nat -C POSTROUTING -s 10.200.0.0/16 -o "\$wan" -j MASQUERADE 2>/dev/null \\
  || sudo iptables -t nat -A POSTROUTING -s 10.200.0.0/16 -o "\$wan" -j MASQUERADE
echo "NAT: 10.200.0.0/16 -> \$wan"
# The CDP proxy is chromebox-proxy, installed by services.sh. Remove the M2
# unit that ran it before, which would fight it for :9222.
if [ -f /etc/systemd/system/chromebox-gw.service ]; then
  sudo systemctl disable --now chromebox-gw >/dev/null 2>&1 || true
  sudo rm -f /etc/systemd/system/chromebox-gw.service
  sudo systemctl daemon-reload
  sudo systemctl reset-failed chromebox-gw 2>/dev/null || true
fi
EOS

for n in $nodes; do
  n_ip=$(priv "$n")
  echo "== $n ($n_ip)"
  scripts/aws/nodes.sh ssh "$n" "NAME=$n NODE_IP=$n_ip GW_IP=$gw_ip bash -s" <<'EOS'
set -euo pipefail
sudo modprobe ip_gre
# Forward between the tunnel and the VMs' TAPs; no strict reverse-path filtering.
printf 'net.ipv4.ip_forward = 1\nnet.ipv4.conf.all.rp_filter = 0\nnet.ipv4.conf.default.rp_filter = 0\n' | sudo tee /etc/sysctl.d/90-chromebox.conf >/dev/null
sudo sysctl -q -p /etc/sysctl.d/90-chromebox.conf 2>/dev/null || true
ip link show gre-gw >/dev/null 2>&1 || sudo ip tunnel add gre-gw mode gre local "$NODE_IP" remote "$GW_IP" ttl 255
sudo ip link set gre-gw up
# Packets for a VM are routed normally (pref 90); everything else a VM sends
# goes into the tunnel (pref 100, table 100).
ip rule show | grep -q 'to 10.200.0.0/16 lookup main' || sudo ip rule add pref 90 to 10.200.0.0/16 lookup main
ip rule show | grep -q 'from 10.200.0.0/16 lookup 100' || sudo ip rule add pref 100 from 10.200.0.0/16 lookup 100
sudo ip route replace default dev gre-gw table 100
# A VM's address with no VM behind it is a dead end, not a hairpin: without this,
# packets for a departed VM would match the default route and leave the node.
sudo ip route replace unreachable 10.200.0.0/16 metric 500
printf '{"name":"%s","gateway":"ec2-user@%s"}\n' "$NAME" "$GW_IP" | sudo tee /var/lib/chromebox/node.json >/dev/null
echo "tunnel gre-gw -> $GW_IP; node.json written"
EOS
done
