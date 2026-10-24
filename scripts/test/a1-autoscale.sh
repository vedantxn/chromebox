#!/usr/bin/env bash
# Autoscaling, end to end, with real machines. Start with node-b stopped.
#  1. fill node-a (5 VMs, its capacity): the orchestrator starts node-b by itself;
#  2. a 5th session through the public SDK is refused for capacity while node-b
#     boots, waits (Retry-After), and lands on node-b;
#  3. two of node-a's VMs end: node-b's VM fits on node-a with room to spare, so
#     node-b is drained with live migration under a running client, and then,
#     empty for the idle time, stopped.
# Needs CHROMEBOX_TOKEN, CHROMEBOX_API, CB (a ctl binary), BATON_API_KEY (a
# test account) and the SDK installed in $SDK_DIR. Run with the gateway's
# CHROMEBOX_AUTOSCALE_COOLDOWN and CHROMEBOX_IDLE_STOP set short (30s, 60s).
set -euo pipefail
cd "$(dirname "$0")/../.."
gw=$(scripts/aws/nodes.sh ip gateway)
log() { echo "$(date -u +%T) $*"; }
nodes() { $CB ctl nodes | awk 'NR>1 {printf "%s(up=%s,vms=%s,%s) ", $1, $2, $4, ($3=="true"?"cordoned":"open")}'; echo; }

log "start: $(nodes)"
declare -a ids
for i in 1 2 3 4 5; do
  read -r id idx < <($CB ctl create | awk '{print $1, $5}')
  ids+=("$id")
done
log "node-a is full (5 VMs). The autoscaler should now start node-b."

log "creating a 6th session through the SDK (it must wait for node-b)"
cat > "$SDK_DIR/a1-create.mjs" <<'JS'
import { Baton } from 'batonvm';
const t = performance.now();
const s = await new Baton().sessions.create();
console.log(`session ${s.id} ready on ${s.node} after ${Math.round((performance.now() - t) / 1000)} s`);
JS
(cd "$SDK_DIR" && node a1-create.mjs) | while read -r l; do log "SDK: $l"; done
log "now: $(nodes)"

# A client on the VM that is on node-b.
read -r vm5 idx5 on < <($CB ctl ls | awk 'NR>1 && $2=="node-b" {print $1, $4, $2}')
log "VM $vm5 runs on $on; attaching a live client (CDP request every 100 ms) to it"
node scripts/demo/soak.mjs "$gw" "$idx5" 240 > /tmp/a1-soak.log 2>&1 &
soak=$!
until grep -q connected /tmp/a1-soak.log; do sleep 1; done

log "ending two VMs on node-a: node-b's VM now fits there with room to spare"
$CB ctl rm "${ids[0]}" >/dev/null; $CB ctl rm "${ids[1]}" >/dev/null
for i in $(seq 60); do
  sleep 5
  where=$($CB ctl ls | awk -v id="$vm5" '$1==id {print $2}')
  [ "$where" = node-a ] && { log "VM $vm5 moved to node-a"; break; }
done
log "now: $(nodes)"
log "waiting for node-b to be stopped (empty for the idle time)..."
for i in $(seq 60); do
  sleep 5
  up=$($CB ctl nodes | awk '$1=="node-b" {print $2}')
  [ "$up" = false ] && { log "node-b is down"; break; }
done
log "now: $(nodes)"
wait $soak || true
echo "--- what the client on the moved VM saw:"
grep -E "requests answered|longest stall|gap between|connection closed|RESULT" /tmp/a1-soak.log
for id in $($CB ctl ls | awk 'NR>1 {print $1}'); do $CB ctl rm "$id" >/dev/null; done
