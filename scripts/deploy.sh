#!/usr/bin/env bash
# Cross-compile chromebox for Linux on this machine and install it on a node.
# Usage: scripts/deploy.sh node-a
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/aws/env.sh
node=${1:?usage: deploy.sh <node>}

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/chromebox ./cmd/chromebox
ip=$(scripts/aws/nodes.sh ip "$node")
scp -q -i "$CB_SSH_KEY" bin/chromebox "ec2-user@$ip:/tmp/chromebox"
scripts/aws/nodes.sh ssh "$node" 'sudo install -m 0755 /tmp/chromebox /usr/local/bin/chromebox'
echo "deployed to $node ($ip)"
