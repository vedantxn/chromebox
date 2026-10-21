#!/usr/bin/env bash
# M3 C1 checks, run on a node that has the base "chrome-v1":
# clones are separate VMs (own address, own disk, own random numbers), and a
# snapshot restored under another id gets that id's own disk.
set -uo pipefail
V=/var/lib/chromebox/vms
fd_inode() { # inode of the file a VM's Firecracker has open as its drive
  local pid; pid=$(cat "$V/$1/fc.pid")
  for fd in /proc/$pid/fd/*; do
    if [[ $(readlink "$fd") == /run/chromebox/drive/rootfs.ext4 ]]; then stat -L -c %i "$fd"; return; fi
  done
}
for v in c1 c2 c1b; do chromebox kill $v >/dev/null 2>&1; rm -rf $V/$v /dev/shm/chromebox/$v; done

echo "== two clones of one base, side by side"
chromebox clone c1 chrome-v1 net=1 | tail -1
chromebox clone c2 chrome-v1 net=2 | tail -1
for v in c1 c2; do
  echo "$v: drive open in Firecracker = inode $(fd_inode $v), its own disk file = inode $(stat -c %i $V/$v/rootfs.ext4)"
done
chromebox guest-ping c1; chromebox guest-ping c2

echo; echo "== a snapshot restored under another id gets that id's disk (this was the T4 hazard in M2)"
sleep 1
chromebox snapshot c1 nosync >/dev/null
chromebox kill c1
mkdir -p $V/c1b /dev/shm/chromebox/c1b
cp -r $V/c1/snap $V/c1b/snap; cp -r /dev/shm/chromebox/c1/. /dev/shm/chromebox/c1b/
chromebox accept c1b | head -c 120; echo
echo "c1b: drive open = inode $(fd_inode c1b), its own disk file = inode $(stat -c %i $V/c1b/rootfs.ext4), c1's file = inode $(stat -c %i $V/c1/rootfs.ext4)"
for v in c2 c1b; do chromebox kill $v >/dev/null 2>&1; rm -rf $V/$v /dev/shm/chromebox/$v; done
rm -rf $V/c1 /dev/shm/chromebox/c1
