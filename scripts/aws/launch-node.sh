#!/usr/bin/env bash
# Launch one chromebox instance. Nodes (VM hosts) are m8i.large with nested
# virtualization enabled; the gateway needs no virtualization and is a small
# t3 instance.
# Usage: scripts/aws/launch-node.sh node-a
#        scripts/aws/launch-node.sh gateway t3.small
set -euo pipefail
source "$(dirname "$0")/env.sh"
name=${1:?usage: launch-node.sh <name> [instance-type]}
type=${2:-$CB_INSTANCE_TYPE}
cpu_options=()
[[ $type == *8i.* ]] && cpu_options=(--cpu-options NestedVirtualization=enabled)

# Nodes get the storage role (paused sessions, profiles); the gateway gets the
# one the autoscaler needs. Both are made by scripts/aws/storage.sh.
profile=chromebox-node
[[ $name == gateway ]] && profile=chromebox-gateway

# Resolve the AMI once and pin it. Every node must boot the same image, because
# a snapshot taken under one host kernel is not guaranteed to load under another.
if [[ ! -s "$CB_AMI_FILE" ]]; then
  aws ssm get-parameter --name "$CB_AMI_PARAM" --query Parameter.Value --output text > "$CB_AMI_FILE"
  echo "pinned AMI $(cat "$CB_AMI_FILE")"
fi
ami=$(cat "$CB_AMI_FILE")

subnet=$(aws ec2 describe-subnets --filters Name=availability-zone,Values="$CB_AZ" Name=default-for-az,Values=true \
  --query 'Subnets[0].SubnetId' --output text)
sg=$(aws ec2 describe-security-groups --filters Name=group-name,Values="$CB_SG_NAME" \
  --query 'SecurityGroups[0].GroupId' --output text)

id=$(aws ec2 run-instances \
  --image-id "$ami" \
  --instance-type "$type" \
  "${cpu_options[@]}" \
  --key-name "$CB_KEY_NAME" \
  --iam-instance-profile "Name=$profile" \
  --subnet-id "$subnet" \
  --security-group-ids "$sg" \
  --block-device-mappings 'DeviceName=/dev/xvda,Ebs={VolumeSize=30,VolumeType=gp3,DeleteOnTermination=true}' \
  --metadata-options HttpTokens=required \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=chromebox-$name},{Key=Project,Value=chromebox}]" \
                       "ResourceType=volume,Tags=[{Key=Project,Value=chromebox}]" \
  --query 'Instances[0].InstanceId' --output text)
echo "launched $id, waiting for it to run..."
aws ec2 wait instance-running --instance-ids "$id"
aws ec2 describe-instances --instance-ids "$id" \
  --query 'Reservations[0].Instances[0].[InstanceId,PublicIpAddress,PrivateIpAddress,CpuOptions.NestedVirtualization]' --output text
