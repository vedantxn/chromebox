#!/usr/bin/env bash
# The headline demo: an agent clicks through Wikipedia in a VM on node-a, and
# node-a is drained under it. The agent's steps keep coming, none fails, its
# connection stays open, and its page state is still there.
# Needs CHROMEBOX_TOKEN, CHROMEBOX_API and CB (a ctl binary) in the environment.
set -euo pipefail
cd "$(dirname "$0")/../.."
gw=$(scripts/aws/nodes.sh ip gateway)
$CB ctl cordon node-b >/dev/null
read -r id idx < <($CB ctl create | awk '{print $1, $5}')
$CB ctl uncordon node-b >/dev/null
echo "$id on node-a"
log=$(mktemp)
node scripts/demo/browse.mjs "$gw" "$idx" 50 >"$log" 2>&1 &
agent=$!
until grep -q "step   5 " "$log"; do kill -0 $agent || { cat "$log"; exit 1; }; sleep 1; done
echo "$(date -u +%T.%3N) draining node-a"
$CB ctl drain node-a | grep -E '"(to|paused_ms|unreachable_ms|stream_ms)"' | tr -s ' \n' ' '; echo
echo "$(date -u +%T.%3N) drained"
wait $agent || true
cat "$log"
$CB ctl uncordon node-a >/dev/null
$CB ctl rm "$id" >/dev/null
rm -f "$log"
