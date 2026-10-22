#!/usr/bin/env bash
# Give the gateway a fixed public address (an Elastic IP) and put CloudFront in
# front of it: a stable https://xxxx.cloudfront.net that carries both the
# orchestrator API (/v1/...) and the browsers' WebSockets (/cdp/...), with TLS
# handled by AWS. Prints the hostname. Safe to re-run.
set -euo pipefail
cd "$(dirname "$0")/../.."
source scripts/aws/env.sh

gw_id=$(aws ec2 describe-instances --filters Name=tag:Name,Values=chromebox-gateway Name=instance-state-name,Values=running \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)
alloc=$(aws ec2 describe-addresses --filters Name=tag:Project,Values=chromebox --query 'Addresses[0].AllocationId' --output text)
if [[ $alloc == None ]]; then
  alloc=$(aws ec2 allocate-address --domain vpc --tag-specifications 'ResourceType=elastic-ip,Tags=[{Key=Project,Value=chromebox}]' \
    --query AllocationId --output text)
  echo "allocated Elastic IP $alloc"
fi
aws ec2 associate-address --allocation-id "$alloc" --instance-id "$gw_id" --allow-reassociation >/dev/null
eip=$(aws ec2 describe-addresses --allocation-ids "$alloc" --query 'Addresses[0].PublicIp' --output text)
origin="ec2-${eip//./-}.compute-1.amazonaws.com"
echo "gateway: $eip ($origin)"

# Only CloudFront (and this machine, already allowed) may reach the API and proxy ports.
sg=$(aws ec2 describe-security-groups --filters Name=group-name,Values="$CB_SG_NAME" --query 'SecurityGroups[0].GroupId' --output text)
pl=$(aws ec2 describe-managed-prefix-lists --filters Name=prefix-list-name,Values=com.amazonaws.global.cloudfront.origin-facing \
  --query 'PrefixLists[0].PrefixListId' --output text)
# One rule for both ports: the CloudFront address list counts as ~55 rules
# against the security group's limit of 60, so a second rule does not fit.
aws ec2 authorize-security-group-ingress --group-id "$sg" \
  --ip-permissions "IpProtocol=tcp,FromPort=8080,ToPort=8081,PrefixListIds=[{PrefixListId=$pl}]" >/dev/null 2>&1 || true

existing=$(aws cloudfront list-distributions --query "DistributionList.Items[?Comment=='baton'].DomainName | [0]" --output text 2>/dev/null || echo None)
if [[ $existing != None && -n $existing ]]; then echo "cloudfront: https://$existing"; exit 0; fi

# No caching (every request is dynamic), every header passed through (WebSocket
# upgrades need them), both origins on the same host, /cdp/* to the proxy.
cfg=$(mktemp)
cat > "$cfg" <<JSON
{
  "CallerReference": "baton-$(date +%s)",
  "Comment": "baton",
  "Enabled": true,
  "PriceClass": "PriceClass_100",
  "Origins": { "Quantity": 2, "Items": [
    { "Id": "api", "DomainName": "$origin",
      "CustomOriginConfig": { "HTTPPort": 8080, "HTTPSPort": 443, "OriginProtocolPolicy": "http-only",
        "OriginReadTimeout": 60, "OriginKeepaliveTimeout": 60 } },
    { "Id": "cdp", "DomainName": "$origin",
      "CustomOriginConfig": { "HTTPPort": 8081, "HTTPSPort": 443, "OriginProtocolPolicy": "http-only",
        "OriginReadTimeout": 60, "OriginKeepaliveTimeout": 60 } } ] },
  "DefaultCacheBehavior": { "TargetOriginId": "api", "ViewerProtocolPolicy": "https-only",
    "AllowedMethods": { "Quantity": 7, "Items": ["GET","HEAD","OPTIONS","PUT","POST","PATCH","DELETE"],
      "CachedMethods": { "Quantity": 2, "Items": ["GET","HEAD"] } },
    "CachePolicyId": "4135ea2d-6df8-44a3-9df3-4b5a84be39ad",
    "OriginRequestPolicyId": "216adef6-5c7f-47e4-b989-5492eafa07d3", "Compress": false },
  "CacheBehaviors": { "Quantity": 1, "Items": [
    { "PathPattern": "/cdp/*", "TargetOriginId": "cdp", "ViewerProtocolPolicy": "https-only",
      "AllowedMethods": { "Quantity": 2, "Items": ["GET","HEAD"], "CachedMethods": { "Quantity": 2, "Items": ["GET","HEAD"] } },
      "CachePolicyId": "4135ea2d-6df8-44a3-9df3-4b5a84be39ad",
      "OriginRequestPolicyId": "216adef6-5c7f-47e4-b989-5492eafa07d3", "Compress": false } ] }
}
JSON
domain=$(aws cloudfront create-distribution --distribution-config "file://$cfg" --query 'Distribution.DomainName' --output text)
rm -f "$cfg"
echo "cloudfront: https://$domain (takes a few minutes to deploy)"
