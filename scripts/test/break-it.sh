#!/usr/bin/env bash
# Break-it session for M1. Runs on node A; node B is the ssh target ($1).
# Each test sets up a situation, does the "wrong" thing, and shows what happens.
# Usage: break-it.sh ec2-user@172.31.2.181 [t1 t2c t2 t3 t4 t5 t6]   (default: all)
B=${1:?usage: break-it.sh <ssh target of node B>}
V=/var/lib/chromebox/vms
K="-i $HOME/.ssh/chromebox_node -o IdentitiesOnly=yes"
onb() { ssh $K "$B" "$@"; }
section() { printf '\n=== %s\n' "$*"; }
cleanup() { for i in "$@"; do chromebox kill "$i" 2>/dev/null; rm -rf "$V/$i"; onb "chromebox kill $i 2>/dev/null; rm -rf $V/$i" 2>/dev/null; done; }
last_count() { grep -a '^count' "$V/$1/serial.log" | tail -1 | tr -d '\r'; }

TESTS=("${@:2}")

if [ ${#TESTS[@]} -eq 0 ] || [[ " ${TESTS[*]} " == *" t1 "* ]]; then
section "T1 clean migration: is the disk consistent afterwards?"
cleanup c1; chromebox boot c1 >/dev/null; sleep 1
chromebox migrate c1 "$B" | grep -E "last count|DOWNTIME"
echo "(letting the VM run 50 s on B so the guest flushes count.log to disk)"; sleep 50
onb "chromebox kill c1"; onb "bash check-count-log.sh $V/c1/rootfs.ext4"
cleanup c1
fi

if [ ${#TESTS[@]} -eq 0 ] || [[ " ${TESTS[*]} " == *" t2c "* ]]; then
section "T2 control: restore with the RIGHT disk, run 45 s"
cleanup s1; chromebox boot s1 >/dev/null; sleep 40
chromebox snapshot s1 nosync >/dev/null; chromebox kill s1
chromebox restore s1 | tail -1; sleep 45; chromebox kill s1
bash check-count-log.sh $V/s1/rootfs.ext4; grep -ac "EXT4-fs error" $V/s1/serial.log | sed 's/^/EXT4-fs error lines in console: /'
cleanup s1
fi

if [ ${#TESTS[@]} -eq 0 ] || [[ " ${TESTS[*]} " == *" t2 "* ]]; then
section "T2 break: restore memory from t=40 s with a STALE disk from t=3 s"
cleanup s2; chromebox boot s2 >/dev/null; sleep 3
chromebox snapshot s2 nosync >/dev/null; cp "$V/s2/snap/rootfs.ext4" /tmp/stale.ext4
chromebox resume s2; sleep 37
chromebox snapshot s2 nosync >/dev/null; chromebox kill s2
cp /tmp/stale.ext4 "$V/s2/snap/rootfs.ext4"; rm -f /tmp/stale.ext4
chromebox restore s2 | tail -1; sleep 45; chromebox kill s2
bash check-count-log.sh $V/s2/rootfs.ext4; grep -ac "EXT4-fs error" $V/s2/serial.log | sed 's/^/EXT4-fs error lines in console: /'
grep -a "EXT4-fs" $V/s2/serial.log | head -3 | cut -c1-160
cleanup s2
fi

if [ ${#TESTS[@]} -eq 0 ] || [[ " ${TESTS[*]} " == *" t3 "* ]]; then
section "T3 truncated memory file"
cleanup t3; chromebox boot t3 >/dev/null; sleep 1; chromebox snapshot t3 nosync >/dev/null; chromebox kill t3
truncate -s 100M $V/t3/snap/mem
chromebox restore t3 2>&1 | head -3; echo "(exit code ${PIPESTATUS[0]})"
cleanup t3
fi

if [ ${#TESTS[@]} -eq 0 ] || [[ " ${TESTS[*]} " == *" t4 "* ]]; then
section "T4 snapshot restored under a different VM id (drive path differs from the one saved in vmstate)"
cleanup t4 t4b; chromebox boot t4 >/dev/null; sleep 1; chromebox snapshot t4 nosync >/dev/null; chromebox kill t4
mkdir -p $V/t4b; cp -r $V/t4/snap $V/t4b/snap
chromebox accept t4b 2>&1 | head -3
cleanup t4 t4b
fi

if [ ${#TESTS[@]} -eq 0 ] || [[ " ${TESTS[*]} " == *" t5 "* ]]; then
section "T5 load a snapshot into a Firecracker that was already configured"
cleanup t5; chromebox boot t5 >/dev/null; sleep 1; chromebox snapshot t5 nosync >/dev/null; chromebox kill t5
rm -f /tmp/t5.sock; firecracker --api-sock /tmp/t5.sock >/dev/null 2>&1 & fc=$!
sleep 0.3
curl -s -X PUT --unix-socket /tmp/t5.sock http://localhost/machine-config -d '{"vcpu_count":1,"mem_size_mib":256}'
curl -s -w ' [HTTP %{http_code}]\n' -X PUT --unix-socket /tmp/t5.sock http://localhost/snapshot/load \
  -d "{\"snapshot_path\":\"$V/t5/snap/vmstate\",\"mem_backend\":{\"backend_type\":\"File\",\"backend_path\":\"$V/t5/snap/mem\"},\"resume_vm\":true}"
kill $fc; cleanup t5
fi

if [ ${#TESTS[@]} -eq 0 ] || [[ " ${TESTS[*]} " == *" t6 "* ]]; then
section "T6 target fails after the pause: does the source survive?"
onb "sudo mv /usr/local/bin/chromebox /usr/local/bin/chromebox.off"
cleanup r1; chromebox boot r1 >/dev/null; sleep 1
chromebox migrate r1 "$B" 2>&1 | head -2 | cut -c1-200
before=$(last_count r1); sleep 1.5; after=$(last_count r1)
echo "source after failure: '$before' then 1.5 s later '$after' (counting = it was resumed)"
echo "state: $(chromebox status r1)"
echo "files left on target: $(onb "ls $V 2>/dev/null | grep -c '^r1$'")"
onb "sudo mv /usr/local/bin/chromebox.off /usr/local/bin/chromebox"
cleanup r1
fi

