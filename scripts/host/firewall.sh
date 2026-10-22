#!/usr/bin/env bash
# Firewall rules that must exist before strangers can run browsers. Runs from
# the dev machine against every running instance. Safe to re-run.
#   gateway: VM traffic may not reach the instance metadata service (where AWS
#            credentials live), the VPC, other private ranges, other VMs, or mail
#            ports; VMs may not open connections to the gateway itself.
#   nodes:   VMs may not open connections to the node (agent API, sshd), nor
#            reach metadata, the VPC or other VMs directly.
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh
names=$(aws ec2 describe-instances --filters Name=tag:Project,Values=chromebox Name=instance-state-name,Values=running \
  --query 'Reservations[].Instances[].Tags[?Key==`Name`]|[].Value' --output text | tr '\t' '\n' | sed 's/^chromebox-//' | sort)

GATEWAY_RULES='
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
    ip saddr 10.200.0.0/16 counter drop
  }
}'
NODE_RULES='
table inet baton {
  chain input { type filter hook input priority 0; policy accept;
    iifname "fc*" ct state established,related accept
    iifname "fc*" counter drop
  }
  chain forward { type filter hook forward priority 0; policy accept;
    ct state established,related accept   # replies to connections the gateway opened (the CDP proxy)
    iifname "fc*" ip daddr { 169.254.0.0/16, 172.16.0.0/12, 10.0.0.0/8, 192.168.0.0/16 } counter drop
  }
}'

for n in $names; do
  rules=$NODE_RULES; [[ $n == gateway ]] && rules=$GATEWAY_RULES
  echo "== $n"
  scripts/aws/nodes.sh ssh "$n" "RULES='$rules' bash -s" <<'EOS'
set -euo pipefail
command -v nft >/dev/null || sudo dnf install -y -q nftables >/dev/null
sudo nft delete table inet baton 2>/dev/null || true
printf '%s\n' "$RULES" | sudo nft -f -
echo "baton firewall loaded: $(sudo nft list table inet baton | grep -c drop) drop rules"
EOS
done
