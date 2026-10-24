#!/usr/bin/env bash
# A small client in us-east-1 for benchmarks, so that numbers measure Baton and
# not the distance to the person running them. It sits in the nodes' security
# group (so it reaches the orchestrator and the gateway on their private IPs,
# as an internal client) and goes through Vercel and CloudFront for the
# public-path test, as a customer would. Tagged Project=chromebox-bench, not
# chromebox, so the node scripts (overlay, firewall) never treat it as a node.
#
#   scripts/aws/bench-client.sh up               launch or start it, install Node
#   scripts/aws/bench-client.sh run [tests...]   copy the harness, run it, fetch the JSON
#   scripts/aws/bench-client.sh down             stop it (it costs ~$0.02/h running)
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh
NAME=chromebox-bench-client

instance() {
  aws ec2 describe-instances --filters "Name=tag:Name,Values=$NAME" "Name=instance-state-name,Values=pending,running,stopping,stopped" \
    --query 'Reservations[0].Instances[0].InstanceId' --output text
}
ip() {
  aws ec2 describe-instances --instance-ids "$(instance)" --query 'Reservations[0].Instances[0].PublicIpAddress' --output text
}
on() { ssh -i "$CB_SSH_KEY" -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 "ec2-user@$(ip)" "$@"; }

case "${1:-}" in
up)
  id=$(instance)
  if [[ $id == None || -z $id ]]; then
    subnet=$(aws ec2 describe-subnets --filters Name=availability-zone,Values="$CB_AZ" Name=default-for-az,Values=true \
      --query 'Subnets[0].SubnetId' --output text)
    sg=$(aws ec2 describe-security-groups --filters Name=group-name,Values="$CB_SG_NAME" --query 'SecurityGroups[0].GroupId' --output text)
    id=$(aws ec2 run-instances --image-id "$(cat "$CB_AMI_FILE")" --instance-type t3.small --key-name "$CB_KEY_NAME" \
      --subnet-id "$subnet" --security-group-ids "$sg" --metadata-options HttpTokens=required \
      --block-device-mappings 'DeviceName=/dev/xvda,Ebs={VolumeSize=16,VolumeType=gp3,DeleteOnTermination=true}' \
      --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$NAME},{Key=Project,Value=chromebox-bench}]" \
      --query 'Instances[0].InstanceId' --output text)
    echo "launched $id"
  else
    aws ec2 start-instances --instance-ids "$id" >/dev/null
  fi
  aws ec2 wait instance-running --instance-ids "$id"
  until on true 2>/dev/null; do sleep 3; done
  on 'command -v node >/dev/null || sudo dnf install -y -q nodejs >/dev/null; mkdir -p bench && cd bench && [ -d node_modules/playwright-core ] || npm install -s playwright-core@1.57.0 >/dev/null; node --version'
  echo "bench client up at $(ip)"
  ;;
run)
  shift
  scp -q -i "$CB_SSH_KEY" scripts/bench/run.mjs "ec2-user@$(ip):bench/run.mjs"
  gw=$(aws ec2 describe-instances --filters Name=tag:Name,Values=chromebox-gateway --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text)
  # The orchestrator token and a throwaway API key travel in the environment of
  # this one command; nothing is written on the client.
  on "cd bench && ORCH=http://$gw:8080 GW=$gw TOKEN=$(cat scripts/aws/token) API=https://batonvm.vercel.app KEY=${BATON_KEY:-} node run.mjs $* > results.json && cat results.json" > "bench/results/$(date -u +%F).json"
  echo "wrote bench/results/$(date -u +%F).json"
  ;;
browserbench|arena)
  # An outside benchmark against Baton, as a free-account customer: Steel's
  # (browserbench) or Notte's Browser Arena (arena). Starts the run in the
  # background on the client with a throwaway account; "<name>-wait" waits for
  # it, fetches the results and deletes the account.
  name=$1; dir=$name; [[ $name == arena ]] && dir=browserarena
  runs=${2:-$([[ $name == arena ]] && echo 100 || echo 300)}
  gw=$(aws ec2 describe-instances --filters Name=tag:Name,Values=chromebox-gateway --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text)
  on 'command -v git >/dev/null || sudo dnf install -y -q git >/dev/null; mkdir -p bb'
  scp -q -i "$CB_SSH_KEY" scripts/bench/$dir/run.sh scripts/bench/$dir/baton.patch "ec2-user@$(ip):bb/"
  on "cd bb && rm -f baton.jsonl run.log && TOKEN=$(cat scripts/aws/token) GW=$gw RUNS=$runs bash -s" <<'EOS'
set -euo pipefail
api() { curl -fsS -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' "$@"; }
uid=$(api -X POST "http://$GW:8080/v1/users" -d '{"github_id":990000001,"login":"browserbench"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])')
key=$(api -X POST "http://$GW:8080/v1/users/$uid/keys" -d '{"name":"browserbench"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["key"])')
echo "$uid" > uid
BATON_API_KEY=$key RUNS=$RUNS nohup bash run.sh > run.log 2>&1 < /dev/null &
echo "started: user $uid, $RUNS runs"
EOS
  ;;
browserbench-wait|arena-wait)
  name=${1%-wait}; out="bench/results/$name-baton-$(date -u +%F).jsonl"
  until ! on 'pgrep -f "bash [r]un.sh" >/dev/null'; do sleep 20; done
  scp -q -i "$CB_SSH_KEY" "ec2-user@$(ip):bb/baton.jsonl" "$out"
  uid=$(on 'cat bb/uid')
  # Delete the throwaway account: its keys, sessions, events and the user row.
  scripts/aws/nodes.sh ssh gateway "sudo python3 - $uid" <<'EOS'
import sqlite3, sys
uid = sys.argv[1]
db = sqlite3.connect('/var/lib/chromebox/orchestrator.db')
db.execute("DELETE FROM events WHERE session_id IN (SELECT id FROM sessions WHERE user_id = ?)", (uid,))
for t in ('sessions', 'api_keys', 'profiles'):
    try: db.execute(f"DELETE FROM {t} WHERE user_id = ?", (uid,))
    except sqlite3.Error: pass
db.execute("DELETE FROM users WHERE id = ?", (uid,))
db.commit()
print("deleted", uid)
EOS
  wc -l "$out"
  ;;
down)
  aws ec2 stop-instances --instance-ids "$(instance)" >/dev/null && echo "stopping $(instance)"
  ;;
*)
  echo "usage: bench-client.sh up|run [tests...]|browserbench|arena [runs]|browserbench-wait|arena-wait|down" >&2
  exit 2
  ;;
esac
