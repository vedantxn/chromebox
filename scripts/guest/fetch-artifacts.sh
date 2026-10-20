#!/usr/bin/env bash
# Fetch the guest kernel and base rootfs from Firecracker's CI bucket.
# Runs on a node. Pinned to one CI build so every node gets identical bytes.
set -euo pipefail
S3=https://s3.amazonaws.com/spec.ccfc.min
CI_PREFIX=firecracker-ci/20261016-5732b8331e1e-0/x86_64   # latest CI build on 2026-10-20
KERNEL=vmlinux-6.18.54        # guest kernel 6.18, supported by Firecracker >= v1.16.1
UBUNTU=ubuntu-24.04
STATE_DIR=/var/lib/chromebox

sudo dnf install -y -q squashfs-tools

# Kernel: an uncompressed vmlinux. Firecracker loads it directly; no bootloader.
if [[ ! -s "$STATE_DIR/kernels/vmlinux" ]]; then
  curl -fsSL -o "$STATE_DIR/kernels/vmlinux" "$S3/$CI_PREFIX/$KERNEL"
fi

# Rootfs: CI ships a read-only squashfs; Firecracker wants a block device image,
# so unpack it and repack it as a 1 GiB ext4 file.
if [[ ! -s "$STATE_DIR/images/base-rootfs.ext4" ]]; then
  tmp=$(mktemp -d)
  curl -fsSL -o "$tmp/rootfs.squashfs" "$S3/$CI_PREFIX/$UBUNTU.squashfs"
  sudo unsquashfs -q -d "$tmp/root" "$tmp/rootfs.squashfs"
  truncate -s 1G "$tmp/base-rootfs.ext4"
  sudo mkfs.ext4 -q -d "$tmp/root" -F "$tmp/base-rootfs.ext4"
  mv "$tmp/base-rootfs.ext4" "$STATE_DIR/images/base-rootfs.ext4"
  sudo rm -rf "$tmp"
fi

ls -lh "$STATE_DIR/kernels/vmlinux" "$STATE_DIR/images/base-rootfs.ext4"
file "$STATE_DIR/kernels/vmlinux"
