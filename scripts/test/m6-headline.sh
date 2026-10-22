#!/usr/bin/env bash
# The M6 headline: one browser logs in, is forked into N, and N agents work in
# parallel, each continuing as that user from the page it was on.
# Usage: scripts/test/m6-headline.sh [n]   (default 6; needs node-b for more than 3)
# Needs CHROMEBOX_TOKEN, CHROMEBOX_API and CB (a ctl binary) in the environment.
set -euo pipefail
cd "$(dirname "$0")/../.."
n=${1:-6}
gw=$(scripts/aws/nodes.sh ip gateway)
$CB ctl cordon node-b >/dev/null 2>&1 || true # the parent on node-a; copies spill to node-b
read -r pid pidx < <($CB ctl create | awk '{print $1, $5}')
$CB ctl uncordon node-b >/dev/null 2>&1 || true
(cd scripts/demo && node fork-probe.mjs setup "$gw" "$pidx")
out=$($CB ctl fork "$pid" "$n")
echo "$out" | python3 -c "
import json,sys; d=json.load(sys.stdin)
print(f\"forked {d['parent']} into {len(d['copies'])}: parent paused {d['paused_ms']:.0f} ms, all copies up {d['total_ms']:.0f} ms after the request\")
for s in d.get('spills') or []: print(f\"  image pulled to {s['node']} in {s['ms']:.0f} ms ({s['disk_mib']:.1f} MiB disk, {s['mem_mib']:.0f} MiB memory)\")
"
idx=$(echo "$out" | python3 -c "import json,sys; print(' '.join(str(c['net_index']) for c in json.load(sys.stdin)['copies']))")
(cd scripts/demo && node fork-agents.mjs "$gw" $idx)
$CB ctl ls
for id in $($CB ctl ls | awk 'NR>1{print $1}'); do $CB ctl rm "$id"; done
