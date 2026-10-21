#!/usr/bin/env bash
# Migrate one Chrome VM back and forth between the two nodes while an agent
# keeps a CDP connection busy, and print what the agent saw.
# Run on the dev machine with VM index 1 already booted on node-a:
#   ssh node-a: chromebox boot c1 chrome net=1
# Usage: scripts/test/m2-pingpong.sh [hops]
set -euo pipefail
cd "$(dirname "$0")/../.."
hops=${1:-4}
priv() { aws ec2 describe-instances --filters "Name=tag:Name,Values=chromebox-$1" Name=instance-state-name,Values=running \
  --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text; }
A=$(priv node-a); B=$(priv node-b); gw=$(scripts/aws/nodes.sh ip gateway)
out=$(mktemp)

secs=$((20 + hops * 38))
(cd scripts/demo && node soak.mjs "$gw" 1 "$secs" > "$out" 2>&1) &
sleep 12
src=node-a; dst=$B
for _ in $(seq "$hops"); do
  echo "=== migrate from $src at $(date +%H:%M:%S)"
  scripts/aws/nodes.sh ssh "$src" "chromebox migrate c1 ec2-user@$dst" 2>&1 | grep -E "DOWNTIME|UNREACHABLE|disk changed|rolled back|chromebox:"
  if [[ $src == node-a ]]; then src=node-b; dst=$A; else src=node-a; dst=$B; fi
  sleep 8
done
wait
cat "$out"
