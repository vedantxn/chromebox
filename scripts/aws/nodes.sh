#!/usr/bin/env bash
# Cost hygiene. Only ever touches instances tagged Project=chromebox.
# Usage: nodes.sh status | start [name...] | stop [name...] | ip <name> | ssh <name> [cmd...]
# start/stop with no names act on every chromebox instance.
set -euo pipefail
source "$(dirname "$0")/env.sh"

# ids <state> [name...]: instance ids in that state, optionally only those named.
ids() {
  local state=$1; shift
  local filters=(Name=tag:Project,Values=chromebox Name=instance-state-name,Values="$state")
  if [[ $# -gt 0 ]]; then
    local names=(); for n in "$@"; do names+=("chromebox-$n"); done
    filters+=(Name=tag:Name,Values="$(IFS=,; echo "${names[*]}")")
  fi
  aws ec2 describe-instances --filters "${filters[@]}" \
    --query 'Reservations[].Instances[].InstanceId' --output text
}

case ${1:-status} in
  status)
    aws ec2 describe-instances --filters Name=tag:Project,Values=chromebox \
      Name=instance-state-name,Values=pending,running,stopping,stopped \
      --query 'Reservations[].Instances[].[Tags[?Key==`Name`]|[0].Value,InstanceId,State.Name,PublicIpAddress,PrivateIpAddress]' \
      --output table ;;
  start)
    shift; i=$(ids stopped "$@"); [[ -z "$i" ]] && { echo "nothing stopped"; exit 0; }
    aws ec2 start-instances --instance-ids $i >/dev/null && aws ec2 wait instance-running --instance-ids $i && "$0" status ;;
  stop)
    shift; i=$(ids running "$@"); [[ -z "$i" ]] && { echo "nothing running"; exit 0; }
    aws ec2 stop-instances --instance-ids $i >/dev/null && echo "stopping $i" ;;
  ip)
    aws ec2 describe-instances \
      --filters Name=tag:Name,Values="chromebox-${2:?usage: nodes.sh ip <name>}" Name=instance-state-name,Values=running \
      --query 'Reservations[0].Instances[0].PublicIpAddress' --output text ;;
  ssh)
    name=${2:?usage: nodes.sh ssh <name>}; shift 2
    exec ssh -i "$CB_SSH_KEY" -o StrictHostKeyChecking=accept-new "ec2-user@$("$0" ip "$name")" "$@" ;;
  *) echo "usage: nodes.sh status|start|stop|ip <name>|ssh <name>"; exit 1 ;;
esac
