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

# 5. The jailer (Firecracker's own sandbox launcher, same release as the VMM):
# each VM runs chrooted to a jail holding only its own files, in its own PID
# namespace and cgroup, as $USER. Jails live on XFS (/srv/jailer) so the copy
# of the firecracker binary it makes per VM is cheap. VMs' cgroups go under
# chromebox/, which hands them the cpu, memory and pids controllers.
if [[ "$(jailer --version 2>/dev/null | head -1)" != "Jailer $FC_VERSION" ]]; then
  tmp=$(mktemp -d)
  base="https://github.com/firecracker-microvm/firecracker/releases/download/$FC_VERSION"
  tgz="firecracker-$FC_VERSION-$ARCH.tgz"
  curl -fsSL -o "$tmp/$tgz" "$base/$tgz"
  curl -fsSL -o "$tmp/$tgz.sha256.txt" "$base/$tgz.sha256.txt"
  (cd "$tmp" && sha256sum -c "$tgz.sha256.txt" >/dev/null)
  tar -xzf "$tmp/$tgz" -C "$tmp"
  sudo install -m 0755 "$tmp/release-$FC_VERSION-$ARCH/jailer-$FC_VERSION-$ARCH" /usr/local/bin/jailer
  rm -rf "$tmp"
fi
sudo mkdir -p /srv/jailer
# At every boot: the chromebox/ cgroup (cgroupfs keeps nothing across a
# reboot), and cgroup2 remounted with favordynmods. Moving the jailer into a
# VM's new cgroup waited ~34 ms for an RCU grace period without it, out of
# ~50 ms for the whole jailer; with it the jailer takes ~20 ms.
sudo tee /etc/systemd/system/chromebox-cgroup.service >/dev/null <<'U'
[Unit]
Description=chromebox: cgroup for the VMs
DefaultDependencies=no
After=sys-fs-cgroup.mount
Before=chromebox-agent.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/bin/sh -c 'mount -o remount,favordynmods /sys/fs/cgroup; mkdir -p /sys/fs/cgroup/chromebox; echo "+cpu +memory +pids" > /sys/fs/cgroup/chromebox/cgroup.subtree_control'
[Install]
WantedBy=multi-user.target
U
sudo systemctl daemon-reload
sudo systemctl enable --now chromebox-cgroup.service >/dev/null 2>&1
echo "ok: $(jailer --version | head -1); cgroup chromebox/ controllers: $(cat /sys/fs/cgroup/chromebox/cgroup.subtree_control); $(grep -o favordynmods /proc/mounts | head -1)"

# 6. Keep systemd-logind away from /dev/shm. With RemoveIPC=yes (its default),
# when the last login session of a regular user ends, logind deletes every
# file that user owns in /dev/shm: base memory, fork images, pool slots and
# the memory dirs of running VMs (found when idle pool slots lost theirs
# after an ssh session closed). Running VMs survived because their memory
# stays mapped; anything that opened those files by name later did not.
sudo mkdir -p /etc/systemd/logind.conf.d
printf '[Login]\nRemoveIPC=no\n' | sudo tee /etc/systemd/logind.conf.d/chromebox.conf >/dev/null
sudo systemctl restart systemd-logind
echo "ok: logind RemoveIPC=$(systemd-analyze cat-config systemd/logind.conf 2>/dev/null | grep -E '^RemoveIPC' | tail -1 | cut -d= -f2)"

# 7. The node's network, at every boot (autoscaling starts and stops nodes, so
# nothing may depend on someone running overlay.sh and firewall.sh from a
# laptop). The tunnel to the gateway, the policy routing that sends everything
# a VM originates into it, and the firewall rules that keep VMs away from the
# metadata service, the VPC, other VMs and mail ports. node.json (written by
# overlay.sh) names the node and the gateway. Keep node.nft in step with
# NODE_RULES in firewall.sh.
sudo mkdir -p /etc/chromebox
sudo tee /etc/chromebox/node.nft >/dev/null <<'NFT'
table inet baton {
  chain input { type filter hook input priority 0; policy accept;
    iifname "fc*" ct state established,related accept
    iifname "fc*" counter drop
  }
  chain forward { type filter hook forward priority 0; policy accept;
    ct state established,related accept
    # The connections of the proxy to Chrome (VM port 9223) outlive a migration,
    # but connection tracking on this node does not move with them. Let packets
    # of the bridge through unless they open a connection (no SYN).
    iifname "fc*" tcp sport 9223 tcp flags & syn == 0 accept
    iifname "fc*" ip daddr { 169.254.0.0/16, 172.16.0.0/12, 10.0.0.0/8, 192.168.0.0/16 } counter drop
  }
}
NFT
sudo tee /usr/local/bin/chromebox-node-boot >/dev/null <<'SH'
#!/bin/sh
set -eu
GW=$(sed -n 's/.*"gateway":"ec2-user@\([^"]*\)".*/\1/p' /var/lib/chromebox/node.json)
[ -n "$GW" ] || { echo "node.json names no gateway; run scripts/host/overlay.sh once" >&2; exit 1; }
NODE_IP=$(ip -4 -o route get "$GW" | sed -n 's/.* src \([0-9.]*\).*/\1/p')
modprobe ip_gre
sysctl -q -p /etc/sysctl.d/90-chromebox.conf || true
ip link show gre-gw >/dev/null 2>&1 || ip tunnel add gre-gw mode gre local "$NODE_IP" remote "$GW" ttl 255
ip link set gre-gw up
ip rule show | grep -q 'to 10.200.0.0/16 lookup main' || ip rule add pref 90 to 10.200.0.0/16 lookup main
ip rule show | grep -q 'from 10.200.0.0/16 lookup 100' || ip rule add pref 100 from 10.200.0.0/16 lookup 100
ip route replace default dev gre-gw table 100
ip route replace unreachable 10.200.0.0/16 metric 500
nft delete table inet baton 2>/dev/null || true
nft -f /etc/chromebox/node.nft
echo "node network up: gre-gw $NODE_IP -> $GW, firewall loaded"
SH
sudo chmod 755 /usr/local/bin/chromebox-node-boot
sudo tee /etc/systemd/system/chromebox-node.service >/dev/null <<'U'
[Unit]
Description=chromebox node network (tunnel to the gateway, routing, firewall)
After=network-online.target
Wants=network-online.target
Before=chromebox-agent.service
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/chromebox-node-boot
[Install]
WantedBy=multi-user.target
U
sudo systemctl daemon-reload
sudo systemctl enable chromebox-node.service >/dev/null 2>&1
# The agent starts after the network and the cgroup are ready.
if [ -f /etc/systemd/system/chromebox-agent.service ] && ! grep -q chromebox-node.service /etc/systemd/system/chromebox-agent.service; then
  sudo sed -i 's/^After=network-online.target$/After=network-online.target chromebox-node.service chromebox-cgroup.service\nWants=chromebox-node.service chromebox-cgroup.service/' /etc/systemd/system/chromebox-agent.service
  sudo systemctl daemon-reload
fi
echo "ok: chromebox-node.service enabled"
