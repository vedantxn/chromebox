#!/usr/bin/env bash
# One post-copy migration under a live agent: soak.mjs keeps a CDP connection
# busy (a request every 100 ms) while the VM moves; prints the orchestrator's
# result, both agents' laps and the agent's view (stalls, disconnects).
# Usage: scripts/test/m5-postcopy.sh <vm id> <vm index> [extra soak seconds]
# Needs CHROMEBOX_TOKEN, CHROMEBOX_API and CB (a ctl binary) in the environment.
set -euo pipefail
cd "$(dirname "$0")/../.."
id=${1:?vm id}; idx=${2:?vm index}; secs=${3:-20}
gw=$(scripts/aws/nodes.sh ip gateway)
log=$(mktemp)
node scripts/demo/soak.mjs "$gw" "$idx" "$secs" >"$log" 2>&1 &
soak=$!
until grep -q connected "$log"; do
  kill -0 $soak 2>/dev/null || { echo "soak could not connect:"; cat "$log"; exit 1; }
  sleep 0.5
done
sleep 3
from=$($CB ctl ls | awk -v id="$id" '$1==id {print $2}')
$CB ctl migrate "$id" | grep -E '"(to|paused_ms|unreachable_ms|stream_ms|stream_mib|delta_mib)"'
scripts/aws/nodes.sh ssh "$from" "sudo journalctl -u chromebox-agent --since -1min --no-pager -o cat | sed -n '/migrate $id/,\$p' | grep -v sudo" 2>/dev/null | head -14
wait $soak || true
grep -v "^.*connected;" "$log"
rm -f "$log"
