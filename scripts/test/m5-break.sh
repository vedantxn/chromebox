#!/usr/bin/env bash
# Kill the source mid-stream: right after a post-copy migration resumes the VM
# on the target, SIGKILL the source's agent (which holds the page server). The
# target can never get the rest of the memory. Expected: its handler kills the
# VM, the orchestrator marks it lost within a few seconds, and the live agent
# sees its connection closed instead of hanging.
# Needs CHROMEBOX_TOKEN, CHROMEBOX_API and CB (a ctl binary) in the environment.
set -euo pipefail
cd "$(dirname "$0")/../.."
gw=$(scripts/aws/nodes.sh ip gateway)
read -r id from idx < <($CB ctl create | awk '{print $1, $3, $5}')
echo "created $id on $from (index $idx)"
log=$(mktemp)
node scripts/demo/soak.mjs "$gw" "$idx" 40 >"$log" 2>&1 &
soak=$!
until grep -q connected "$log"; do sleep 0.5; done
# The source agent exits 200 ms after the target resumes (CHROMEBOX_FAULT), its
# page server with it; systemd restarts it without the fault afterwards.
setfault() { scripts/aws/nodes.sh ssh "$from" "sudo sed -i '/^CHROMEBOX_FAULT=/d' /etc/chromebox/env; [ -z '$1' ] || echo CHROMEBOX_FAULT=$1 | sudo tee -a /etc/chromebox/env >/dev/null; sudo systemctl restart chromebox-agent" 2>/dev/null; }
setfault source-dies
sleep 3
$CB ctl migrate "$id" 2>&1 | grep -E '"to"|error' || true
setfault ""
sleep 6
to=$($CB ctl ls | awk -v id="$id" '$1==id {print $2}')
echo "--- orchestrator"
scripts/aws/nodes.sh ssh gateway "sudo journalctl -u chromebox-orchestrator --since -1min --no-pager -o short-precise | grep $id" 2>/dev/null | cut -c17- || true
echo "--- target handler"
for n in node-a node-b; do [[ $n == "$from" ]] || scripts/aws/nodes.sh ssh $n "cat /var/lib/chromebox/vms/$id/uffd.log 2>/dev/null | tail -2" 2>/dev/null; done
wait $soak || true
echo "--- what the agent saw"
grep -E "closed|failed|stall|RESULT|requests answered" "$log"
rm -f "$log"
