#!/usr/bin/env bash
# Check that the counter's log on a VM's disk is an unbroken sequence 1, 2, 3, ...
# Usage: check-count-log.sh <rootfs.ext4>
#
# A VM that was killed (or migrated away) left its disk like a machine that lost
# power: the ext4 journal may hold committed changes not yet applied. So we work
# on a clone and mount it read-write, which makes the kernel replay the journal,
# exactly as the guest would on its next boot. The original is not touched.
set -u
img=$1
clone=$(mktemp /tmp/check.XXXXXX.ext4)
m=$(mktemp -d)
cp --reflink=auto --sparse=always "$img" "$clone"
sudo mount -o loop "$clone" "$m" || { echo "mount failed"; rm -f "$clone"; exit 1; }
f=$m/var/lib/counter/count.log
if [ ! -f "$f" ]; then
  echo "count.log does not exist on this disk"
else
  # A killed VM can leave a torn last line (the final block was only partly
  # written), so the last line is judged separately from the rest.
  awk '{ line[NR] = $0 }
       END {
         n = NR; breaks = 0
         for (i = 1; i < n; i++) if (line[i] != i) { if (!breaks) first = i; breaks++ }
         last = (line[n] == n) ? "complete" : "torn"
         printf "lines on disk: %d, breaks in sequence before the last line: %d", n, breaks
         if (breaks) printf " (first at line %d)", first
         printf ", last line: %s\n", last
       }' "$f"
fi
sudo umount "$m"; rmdir "$m"; rm -f "$clone"
