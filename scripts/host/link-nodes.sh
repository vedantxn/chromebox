#!/usr/bin/env bash
# Let node A SSH into node B, so `chromebox migrate` can drive B from A.
# Runs on the dev machine. A gets its own key pair that never leaves A, and only
# its public half is authorized on B. Safe to re-run.
# Usage: scripts/host/link-nodes.sh node-a node-b
set -euo pipefail
cd "$(dirname "$0")/../.."
a=${1:?usage: link-nodes.sh <from> <to>}; b=${2:?usage: link-nodes.sh <from> <to>}

scripts/aws/nodes.sh ssh "$a" '[ -f ~/.ssh/chromebox_node ] || ssh-keygen -q -t ed25519 -N "" -f ~/.ssh/chromebox_node'
pub=$(scripts/aws/nodes.sh ssh "$a" 'cat ~/.ssh/chromebox_node.pub')
scripts/aws/nodes.sh ssh "$b" "grep -qF '$pub' ~/.ssh/authorized_keys || echo '$pub' >> ~/.ssh/authorized_keys"

b_private=$(aws ec2 describe-instances --filters "Name=tag:Name,Values=chromebox-$b" Name=instance-state-name,Values=running \
  --query 'Reservations[0].Instances[0].PrivateIpAddress' --output text)
scripts/aws/nodes.sh ssh "$a" "ssh -i ~/.ssh/chromebox_node -o StrictHostKeyChecking=accept-new ec2-user@$b_private hostname -s" | sed 's/^/linked: node A can run commands on /'
echo "node-to-node target: ec2-user@$b_private"
