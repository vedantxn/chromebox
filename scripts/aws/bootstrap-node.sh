#!/usr/bin/env bash
# Wait for a freshly launched node to accept SSH, then run host setup on it.
# Usage: scripts/aws/bootstrap-node.sh node-b
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh
name=${1:?usage: bootstrap-node.sh <name>}

ip=$(scripts/aws/nodes.sh ip "$name")
for _ in $(seq 1 40); do
  scripts/aws/nodes.sh ssh "$name" true 2>/dev/null && break
  sleep 5
done
scp -q -i "$CB_SSH_KEY" scripts/host/setup.sh "ec2-user@$ip:setup.sh"
scripts/aws/nodes.sh ssh "$name" 'bash setup.sh'
