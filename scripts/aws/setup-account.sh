#!/usr/bin/env bash
# One-time account setup: import the SSH public key as an EC2 key pair and
# create the security group the nodes share. Safe to re-run.
set -euo pipefail
source "$(dirname "$0")/env.sh"

if ! aws ec2 describe-key-pairs --key-names "$CB_KEY_NAME" >/dev/null 2>&1; then
  aws ec2 import-key-pair --key-name "$CB_KEY_NAME" \
    --public-key-material "fileb://$CB_PUBKEY" \
    --tag-specifications "ResourceType=key-pair,Tags=[{Key=Project,Value=chromebox}]" >/dev/null
  echo "imported key pair $CB_KEY_NAME"
fi

vpc=$(aws ec2 describe-vpcs --filters Name=isDefault,Values=true --query 'Vpcs[0].VpcId' --output text)
sg=$(aws ec2 describe-security-groups --filters Name=group-name,Values="$CB_SG_NAME" Name=vpc-id,Values="$vpc" \
  --query 'SecurityGroups[0].GroupId' --output text)
if [[ "$sg" == "None" ]]; then
  sg=$(aws ec2 create-security-group --group-name "$CB_SG_NAME" --vpc-id "$vpc" \
    --description "chromebox nodes: SSH from the dev machine, all traffic between nodes" \
    --tag-specifications "ResourceType=security-group,Tags=[{Key=Project,Value=chromebox}]" \
    --query GroupId --output text)
  # Nodes may talk to each other on any port (rsync, ssh, later the agent API).
  aws ec2 authorize-security-group-ingress --group-id "$sg" \
    --ip-permissions "IpProtocol=-1,UserIdGroupPairs=[{GroupId=$sg}]" >/dev/null
  echo "created security group $sg"
fi

# SSH only from this machine's current public IP. Re-run when your IP changes.
myip=$(curl -s https://checkip.amazonaws.com)
aws ec2 authorize-security-group-ingress --group-id "$sg" \
  --protocol tcp --port 22 --cidr "$myip/32" >/dev/null 2>&1 \
  && echo "allowed ssh from $myip" || echo "ssh from $myip already allowed"

# The gateway's CDP proxy (agents connect here), also only from this machine.
aws ec2 authorize-security-group-ingress --group-id "$sg" \
  --protocol tcp --port 9222 --cidr "$myip/32" >/dev/null 2>&1 \
  && echo "allowed cdp (9222) from $myip" || echo "cdp from $myip already allowed"

# The orchestrator's API (for `chromebox ctl` run from this machine), also only from here.
aws ec2 authorize-security-group-ingress --group-id "$sg" \
  --protocol tcp --port 8080 --cidr "$myip/32" >/dev/null 2>&1 \
  && echo "allowed api (8080) from $myip" || echo "api from $myip already allowed"
