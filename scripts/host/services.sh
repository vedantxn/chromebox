#!/usr/bin/env bash
# Install and start the chromebox services on every running instance:
#   gateway -> chromebox-orchestrator (API :8080, CDP proxy :9222)
#   nodes   -> chromebox-agent        (API :7000, heartbeats to the orchestrator)
# A shared secret, created on first use and kept in scripts/aws/token (not in
# git), authenticates every call between them. Safe to re-run.
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh

tokfile=scripts/aws/token
[[ -s $tokfile ]] || { umask 077; openssl rand -hex 24 > $tokfile; echo "created $tokfile"; }
token=$(cat $tokfile)

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
    scripts/aws/nodes.sh ssh gateway "TOKEN=$token bash -s" <<'EOS'
set -euo pipefail
sudo mkdir -p /etc/chromebox /var/lib/chromebox && sudo chown ec2-user: /var/lib/chromebox   # the orchestrator's database lives here
printf 'CHROMEBOX_TOKEN=%s\nCHROMEBOX_STRATEGY=spread\n' "$TOKEN" | sudo tee /etc/chromebox/env >/dev/null
sudo chmod 600 /etc/chromebox/env
sudo systemctl disable --now chromebox-gw >/dev/null 2>&1 || true   # the proxy now lives inside the orchestrator
sudo tee /etc/systemd/system/chromebox-orchestrator.service >/dev/null <<'U'
[Unit]
Description=chromebox orchestrator
After=network-online.target
[Service]
User=ec2-user
EnvironmentFile=/etc/chromebox/env
ExecStart=/usr/local/bin/chromebox orchestrator :8080
Restart=always
[Install]
WantedBy=multi-user.target
U
sudo systemctl daemon-reload
sudo systemctl enable chromebox-orchestrator >/dev/null 2>&1
sudo systemctl restart chromebox-orchestrator
sleep 1; echo "orchestrator: $(systemctl is-active chromebox-orchestrator)"
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
