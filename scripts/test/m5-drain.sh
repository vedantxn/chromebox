#!/usr/bin/env bash
# Drain a node under live agents, with post-copy: N VMs on node A (node B is
# cordoned while they are made), one soak.mjs agent per VM, then drain A.
# Usage: scripts/test/m5-drain.sh [n]   (default 4; the global cap is 5)
# Needs CHROMEBOX_TOKEN, CHROMEBOX_API and CB (a ctl binary) in the environment.
set -euo pipefail
cd "$(dirname "$0")/../.."
n=${1:-4}
gw=$(scripts/aws/nodes.sh ip gateway)
$CB ctl cordon node-b >/dev/null
ids=(); idxs=()
for i in $(seq "$n"); do
  read -r id idx < <($CB ctl create | awk '{print $1, $5}')
  ids+=("$id"); idxs+=("$idx")
done
$CB ctl uncordon node-b >/dev/null
echo "made ${ids[*]} on node-a"
dir=$(mktemp -d)
for i in "${!ids[@]}"; do node scripts/demo/soak.mjs "$gw" "${idxs[$i]}" 40 >"$dir/${ids[$i]}" 2>&1 & done
for id in "${ids[@]}"; do until grep -q connected "$dir/$id"; do sleep 0.5; done; done
sleep 3
start=$(date +%s.%N)
$CB ctl drain node-a | grep -E '"(vm|to|paused_ms|unreachable_ms|stream_ms|error)"' | paste - - - - - | sed 's/  */ /g'
echo "drain took $(echo "$(date +%s.%N) - $start" | bc | cut -c1-5) s"
wait
for id in "${ids[@]}"; do echo "$id: $(grep -E 'gap between|closed events' "$dir/$id" | tr -s ' ' | tr '\n' ' ')"; done
for id in "${ids[@]}"; do $CB ctl rm "$id" >/dev/null; done
$CB ctl uncordon node-a >/dev/null # a drain leaves the node cordoned
rm -rf "$dir"
