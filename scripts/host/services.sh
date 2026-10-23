#!/usr/bin/env bash
# Install and start the chromebox services on every running instance:
#   gateway -> chromebox-orchestrator (API :8080) and chromebox-proxy (public :8081, operator :9222)
#   nodes   -> chromebox-agent        (API :7000, heartbeats to the orchestrator)
# A shared secret, created on first use and kept in scripts/aws/token (not in
# git), authenticates every call between them. Safe to re-run.
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh

tokfile=scripts/aws/token
[[ -s $tokfile ]] || { umask 077; openssl rand -hex 24 > $tokfile; echo "created $tokfile"; }
token=$(cat $tokfile)
# Signs the session tokens clients use to reach their browser (checked by the proxy).
secfile=scripts/aws/session-secret
[[ -s $secfile ]] || { umask 077; openssl rand -hex 32 > $secfile; echo "created $secfile"; }
secret=$(cat $secfile)

priv() {
  aws ec2 describe-instances --filters "Name=tag:Name,Values=chromebox-$1" Name=instance-state-name,Values=running \
    --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text
}
gw_ip=$(priv gateway)
names=$(aws ec2 describe-instances --filters Name=tag:Project,Values=chromebox Name=instance-state-name,Values=running \
  --query 'Reservations[].Instances[].Tags[?Key==`Name`]|[].Value' --output text | tr '\t' '\n' | sed 's/^chromebox-//' | sort)

for n in $names; do
  if [[ $n == gateway ]]; then
    echo "== gateway: orchestrator"
    public=$(aws cloudfront list-distributions --query "DistributionList.Items[?Comment=='baton'].DomainName | [0]" --output text)
    scripts/aws/nodes.sh ssh gateway "TOKEN=$token SECRET=$secret PUBLIC=https://$public bash -s" <<'EOS'
set -euo pipefail
sudo mkdir -p /etc/chromebox /var/lib/chromebox && sudo chown ec2-user: /var/lib/chromebox   # the orchestrator's database lives here
printf 'CHROMEBOX_TOKEN=%s\nCHROMEBOX_SESSION_SECRET=%s\nCHROMEBOX_STRATEGY=spread\nCHROMEBOX_MAX_VMS=5\nCHROMEBOX_PUBLIC_URL=%s\n' "$TOKEN" "$SECRET" "$PUBLIC" | sudo tee /etc/chromebox/env >/dev/null
sudo chmod 600 /etc/chromebox/env
sudo systemctl disable --now chromebox-gw >/dev/null 2>&1 || true   # replaced by chromebox-proxy
sudo tee /etc/systemd/system/chromebox-orchestrator.service >/dev/null <<'U'
[Unit]
Description=chromebox orchestrator
After=network-online.target
[Service]
User=ec2-user
# Moving a VM's route is the one privileged thing it does. With the capability
# it runs ip directly: sudo's first call in a service took ~300 ms, all of it
# inside a migration's outage (m5.md).
AmbientCapabilities=CAP_NET_ADMIN
EnvironmentFile=/etc/chromebox/env
ExecStart=/usr/local/bin/chromebox orchestrator :8080
Restart=always
[Install]
WantedBy=multi-user.target
U
sudo tee /etc/systemd/system/chromebox-proxy.service >/dev/null <<'U'
[Unit]
Description=chromebox CDP proxy
After=network-online.target
[Service]
User=ec2-user
EnvironmentFile=/etc/chromebox/env
ExecStart=/usr/local/bin/chromebox proxy :8081
Restart=always
[Install]
WantedBy=multi-user.target
U
sudo systemctl daemon-reload
for u in chromebox-proxy chromebox-orchestrator; do sudo systemctl enable $u >/dev/null 2>&1; sudo systemctl restart $u; done
sleep 1; echo "proxy: $(systemctl is-active chromebox-proxy), orchestrator: $(systemctl is-active chromebox-orchestrator)"
EOS
  else
    echo "== $n: agent"
    scripts/aws/nodes.sh ssh "$n" "TOKEN=$token ORCH=http://$gw_ip:8080 bash -s" <<'EOS'
set -euo pipefail
sudo mkdir -p /etc/chromebox
printf 'CHROMEBOX_TOKEN=%s\nCHROMEBOX_ORCHESTRATOR=%s\n' "$TOKEN" "$ORCH" | sudo tee /etc/chromebox/env >/dev/null
sudo chmod 600 /etc/chromebox/env
sudo tee /etc/systemd/system/chromebox-agent.service >/dev/null <<'U'
[Unit]
Description=chromebox node agent
After=network-online.target
[Service]
User=ec2-user
EnvironmentFile=/etc/chromebox/env
ExecStart=/usr/local/bin/chromebox agent :7000
Restart=always
# A restart stops the agent only. By default systemd kills the whole control
# group, which took the page-fault handlers of VMs still receiving their
# memory after a post-copy migration with it: those VMs could never complete
# and were killed. (Firecracker itself runs in the VM's own cgroup.)
KillMode=process
[Install]
WantedBy=multi-user.target
U
sudo systemctl daemon-reload
sudo systemctl enable chromebox-agent >/dev/null 2>&1
sudo systemctl restart chromebox-agent
sleep 1; echo "agent: $(systemctl is-active chromebox-agent)"
EOS
  fi
done
