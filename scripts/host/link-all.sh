#!/usr/bin/env bash
# Let every running node ssh into every other one (migration data travels over
# ssh). Safe to re-run.
set -euo pipefail
cd "$(dirname "$0")/../.."
nodes=$(aws ec2 describe-instances --filters Name=tag:Project,Values=chromebox Name=instance-state-name,Values=running \
  --query 'Reservations[].Instances[].Tags[?Key==`Name`]|[].Value' --output text | tr '\t' '\n' | sed 's/^chromebox-//' | grep '^node-' | sort)
for a in $nodes; do for b in $nodes; do
  [[ $a == "$b" ]] || { echo "link $a -> $b"; scripts/host/link-nodes.sh "$a" "$b" >/dev/null; }
done; done
