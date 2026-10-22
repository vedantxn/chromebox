#!/usr/bin/env bash
# Prepare a chromebox node: check KVM, install a pinned Firecracker, create dirs.
# Runs on the node as ec2-user (uses sudo). Safe to re-run.
set -euo pipefail
FC_VERSION=v1.17.0
ARCH=x86_64
STATE_DIR=/var/lib/chromebox

# 1. KVM must exist, or nested virtualization isn't on for this instance.
if [[ ! -e /dev/kvm ]]; then
  echo "FAIL: /dev/kvm missing. Was the instance launched with NestedVirtualization=enabled?" >&2
  exit 1
fi
grep -qE 'vmx|svm' /proc/cpuinfo && echo "ok: CPU exposes VT-x (vmx) to this instance"
if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
  sudo usermod -aG kvm "$USER"
  echo "added $USER to the kvm group; log out and back in for it to apply"
fi

# 2. Firecracker, pinned, checked against the release's published checksum.
if [[ "$(firecracker --version 2>/dev/null | head -1)" != "Firecracker $FC_VERSION" ]]; then
  tmp=$(mktemp -d)
  base="https://github.com/firecracker-microvm/firecracker/releases/download/$FC_VERSION"
  tgz="firecracker-$FC_VERSION-$ARCH.tgz"
  curl -fsSL -o "$tmp/$tgz" "$base/$tgz"
  curl -fsSL -o "$tmp/$tgz.sha256.txt" "$base/$tgz.sha256.txt"
  (cd "$tmp" && sha256sum -c "$tgz.sha256.txt")
  tar -xzf "$tmp/$tgz" -C "$tmp"
  sudo install -m 0755 "$tmp/release-$FC_VERSION-$ARCH/firecracker-$FC_VERSION-$ARCH" /usr/local/bin/firecracker
  rm -rf "$tmp"
fi
echo "ok: $(firecracker --version | head -1)"

# 3. Tools the migration needs, and the state directory (same path on every node).
sudo dnf install -y -q rsync
sudo mkdir -p "$STATE_DIR"/{kernels,images,vms}
sudo chown -R "$USER": "$STATE_DIR"
echo "ok: $STATE_DIR ready; host kernel $(uname -r)"

# 4. userfaultfd (M5): a restored VM's memory can be served page by page by our
# handler. Firecracker runs as $USER, and on this kernel it opens
# /dev/userfaultfd, which is root-only by default. Give $USER's group access,
# not everyone: userfaultfd is a known aid to kernel exploits.
echo "KERNEL==\"userfaultfd\", GROUP=\"$(id -gn)\", MODE=\"0660\"" | sudo tee /etc/udev/rules.d/90-userfaultfd.rules >/dev/null
sudo udevadm control --reload
sudo udevadm trigger --name-match=userfaultfd
echo "ok: $(ls -l /dev/userfaultfd)"
