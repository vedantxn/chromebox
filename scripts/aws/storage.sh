#!/usr/bin/env bash
# Storage and permissions for paused sessions and saved profiles (idempotent).
#
#   an S3 bucket        chromebox-snapshots-<account>: private, encrypted, no public access;
#                       objects under sessions/ expire after 7 days, unfinished uploads after 1
#   role chromebox-node attached to the nodes (instance profile of the same name):
#                       read, write, delete and list on that bucket and nothing else
#   role chromebox-gateway  attached to the gateway: start, stop and describe the
#                       instances tagged Project=chromebox (the autoscaler, A1) and nothing else
#
# VMs cannot use these roles: their traffic to the instance metadata service is
# dropped (scripts/host/firewall.sh), and it needs a hop that guest packets never make.
# Usage: scripts/aws/storage.sh
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh

account=$(aws sts get-caller-identity --query Account --output text)
bucket="chromebox-snapshots-$account"

if aws s3api head-bucket --bucket "$bucket" 2>/dev/null; then
  echo "bucket $bucket exists"
else
  aws s3api create-bucket --bucket "$bucket" --region "$AWS_REGION" >/dev/null
  echo "created bucket $bucket"
fi
aws s3api put-public-access-block --bucket "$bucket" \
  --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
aws s3api put-bucket-encryption --bucket "$bucket" \
  --server-side-encryption-configuration '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
aws s3api put-bucket-lifecycle-configuration --bucket "$bucket" --lifecycle-configuration '{"Rules":[
  {"ID":"paused-sessions-expire","Status":"Enabled","Filter":{"Prefix":"sessions/"},"Expiration":{"Days":7}},
  {"ID":"abort-unfinished-uploads","Status":"Enabled","Filter":{"Prefix":""},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":1}}]}'

trust='{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}'
role() { # role <name> <inline policy json>
  aws iam get-role --role-name "$1" >/dev/null 2>&1 || aws iam create-role --role-name "$1" --assume-role-policy-document "$trust" \
    --tags Key=Project,Value=chromebox >/dev/null
  aws iam put-role-policy --role-name "$1" --policy-name "$1" --policy-document "$2"
  aws iam get-instance-profile --instance-profile-name "$1" >/dev/null 2>&1 || {
    aws iam create-instance-profile --instance-profile-name "$1" >/dev/null
    aws iam add-role-to-instance-profile --instance-profile-name "$1" --role-name "$1"
  }
  echo "role and instance profile $1 ready"
}

role chromebox-node '{"Version":"2012-10-17","Statement":[
  {"Effect":"Allow","Action":["s3:GetObject","s3:PutObject","s3:DeleteObject"],"Resource":"arn:aws:s3:::'"$bucket"'/*"},
  {"Effect":"Allow","Action":["s3:ListBucket"],"Resource":"arn:aws:s3:::'"$bucket"'"}]}'

role chromebox-gateway '{"Version":"2012-10-17","Statement":[
  {"Effect":"Allow","Action":["ec2:DescribeInstances"],"Resource":"*"},
  {"Effect":"Allow","Action":["ec2:StartInstances","ec2:StopInstances"],"Resource":"arn:aws:ec2:*:'"$account"':instance/*",
   "Condition":{"StringEquals":{"aws:ResourceTag/Project":"chromebox"}}}]}'

# Attach to the instances that exist (new ones get it from launch-node.sh).
attach() { # attach <instance name> <profile>
  local id; id=$(aws ec2 describe-instances --filters "Name=tag:Name,Values=chromebox-$1" "Name=instance-state-name,Values=pending,running,stopping,stopped" \
    --query 'Reservations[0].Instances[0].InstanceId' --output text)
  [[ $id == None ]] && return 0
  local have; have=$(aws ec2 describe-iam-instance-profile-associations --filters "Name=instance-id,Values=$id" "Name=state,Values=associated,associating" \
    --query 'IamInstanceProfileAssociations[0].IamInstanceProfile.Arn' --output text)
  if [[ $have == None ]]; then
    for i in $(seq 10); do # a new instance profile takes a few seconds to be usable
      aws ec2 associate-iam-instance-profile --instance-id "$id" --iam-instance-profile "Name=$2" >/dev/null 2>&1 && break || sleep 3
    done
    echo "attached $2 to chromebox-$1"
  else
    echo "chromebox-$1 already has ${have##*/}"
  fi
}
for n in node-a node-b node-c; do attach "$n" chromebox-node; done
attach gateway chromebox-gateway

echo "$bucket" > scripts/aws/bucket.txt
echo "bucket name saved in scripts/aws/bucket.txt"
