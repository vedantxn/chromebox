#!/usr/bin/env bash
# Build the Chrome golden image: images/chrome.ext4 = Ubuntu 24.04 + Chrome for
# Testing + the systemd units that start it and say when it is ready.
# Runs on a node, as ec2-user (uses sudo). Takes a few minutes the first time.
#
# Why not extend the CI base image: it is stripped of the package database, so
# apt cannot safely install into it. Ubuntu's own minimal root tarball has a
# complete dpkg database.
set -euo pipefail

UBUNTU_REL=release-20261017
UBUNTU_TAR=ubuntu-24.04-minimal-cloudimg-amd64-root.tar.xz
UBUNTU_SHA=15def31708587e1a6ffc9864caa8203ed81dc0fd68d9aa060e86799bd7ef24bf
CHROME_VERSION=156.0.8078.47     # Chrome for Testing, pinned
IMAGE_SIZE=4G                    # sparse: only blocks in use take space

STATE_DIR=/var/lib/chromebox
BUILD=$STATE_DIR/build
ROOT=$BUILD/chrome-root
OUT=$STATE_DIR/images/chrome.ext4

mkdir -p "$BUILD"
cleanup() {
  for m in dev/pts dev proc sys; do sudo umount "$ROOT/$m" 2>/dev/null || true; done
}
trap cleanup EXIT

# 1. Ubuntu root filesystem, checked against the published checksum.
if [[ ! -s "$BUILD/$UBUNTU_TAR" ]]; then
  curl -fsSL -o "$BUILD/$UBUNTU_TAR" "https://cloud-images.ubuntu.com/minimal/releases/noble/$UBUNTU_REL/$UBUNTU_TAR"
fi
echo "$UBUNTU_SHA  $BUILD/$UBUNTU_TAR" | sha256sum -c -
sudo rm -rf "$ROOT"; sudo mkdir -p "$ROOT"
sudo tar -xpf "$BUILD/$UBUNTU_TAR" -C "$ROOT" --numeric-owner

# 2. Chrome for Testing (the real Chrome, pinned).
CHROME_ZIP=$BUILD/chrome-$CHROME_VERSION.zip
if [[ ! -s "$CHROME_ZIP" ]]; then
  curl -fsSL -o "$CHROME_ZIP" "https://storage.googleapis.com/chrome-for-testing-public/$CHROME_VERSION/linux64/chrome-linux64.zip"
fi
sudo rm -rf "$ROOT/opt/chrome"; sudo mkdir -p "$ROOT/opt"
sudo unzip -q "$CHROME_ZIP" -d "$ROOT/opt"
sudo mv "$ROOT/opt/chrome-linux64" "$ROOT/opt/chrome"

# 3. Install Chrome's shared libraries and the few tools the guest needs, from
#    inside the new root. The node's network is used for apt.
for m in proc sys dev dev/pts; do sudo mount --bind "/$m" "$ROOT/$m"; done
sudo rm -f "$ROOT/etc/resolv.conf"; sudo cp /etc/resolv.conf "$ROOT/etc/resolv.conf"
sudo chroot "$ROOT" env DEBIAN_FRONTEND=noninteractive bash -euxc '
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends \
    ca-certificates curl socat iproute2 iputils-ping \
    fonts-liberation fonts-noto-color-emoji \
    libasound2t64 libatk-bridge2.0-0t64 libatk1.0-0t64 libatspi2.0-0t64 libcairo2 \
    libcups2t64 libdbus-1-3 libdrm2 libgbm1 libglib2.0-0t64 libnspr4 libnss3 \
    libpango-1.0-0 libx11-6 libxcb1 libxcomposite1 libxdamage1 libxext6 libxfixes3 \
    libxkbcommon0 libxrandr2
  # Every library Chrome links against must resolve, or it fails at runtime.
  if ldd /opt/chrome/chrome | grep "not found"; then echo "missing libraries" >&2; exit 1; fi
  # The guest has no use for these and they only slow the boot or touch the network.
  systemctl mask cloud-init.service cloud-init-local.service cloud-config.service cloud-final.service \
    systemd-networkd.service systemd-networkd-wait-online.service systemd-resolved.service \
    apt-daily.timer apt-daily-upgrade.timer motd-news.timer e2scrub_all.timer fstrim.timer \
    snapd.service snapd.socket snapd.seeded.service snapd.apparmor.service pollinate.service \
    ModemManager.service multipathd.service lvm2-monitor.service ua-timer.timer \
    systemd-timedated.service setvtrgb.service console-setup.service keyboard-setup.service
  useradd -m -s /usr/sbin/nologin chrome
  apt-get clean; rm -rf /var/lib/apt/lists/*
'

# 4. Configuration the guest needs at runtime.
# DNS: a fixed resolver; the VM's address and route arrive on the kernel command line.
sudo rm -f "$ROOT/etc/resolv.conf"
printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' | sudo tee "$ROOT/etc/resolv.conf" >/dev/null

# Chrome. The CDP port listens on localhost only; chrome-bridge exposes it on the
# network. The sandbox stays on: Chrome runs as an unprivileged user.
sudo tee "$ROOT/etc/systemd/system/chrome.service" >/dev/null <<'EOF'
[Unit]
Description=Chrome (headless, CDP on 127.0.0.1:9222)

[Service]
User=chrome
ExecStart=/opt/chrome/chrome --headless=new --remote-debugging-port=9222 \
  --user-data-dir=/home/chrome/profile --no-first-run --no-default-browser-check \
  --disable-gpu --disable-dev-shm-usage --disable-background-networking about:blank
Restart=on-failure
StandardOutput=file:/dev/console
StandardError=file:/dev/console

[Install]
WantedBy=multi-user.target
EOF

sudo tee "$ROOT/etc/systemd/system/chrome-bridge.service" >/dev/null <<'EOF'
[Unit]
Description=Expose Chrome's CDP port on the network

[Service]
ExecStart=/usr/bin/socat TCP-LISTEN:9223,fork,reuseaddr TCP:127.0.0.1:9222
Restart=always

[Install]
WantedBy=multi-user.target
EOF

# Readiness: print one line to the console when Chrome answers CDP. The host
# watches the serial log for it.
sudo tee "$ROOT/usr/local/bin/chrome-ready.sh" >/dev/null <<'EOF'
#!/bin/sh
until v=$(curl -sf http://127.0.0.1:9222/json/version); do sleep 0.05; done
echo "chrome ready $(echo "$v" | sed -n 's/.*"Browser": *"\([^"]*\)".*/\1/p')"
EOF
sudo chmod 0755 "$ROOT/usr/local/bin/chrome-ready.sh"
sudo tee "$ROOT/etc/systemd/system/chrome-ready.service" >/dev/null <<'EOF'
[Unit]
Description=Announce that Chrome is ready
After=chrome.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/chrome-ready.sh
StandardOutput=file:/dev/console

[Install]
WantedBy=multi-user.target
EOF
# The guest agent: the host's channel (over vsock) for giving a restored VM its
# own address and the right time. Built on the dev machine and copied to the
# node first: GOOS=linux GOARCH=amd64 go build -o bin/chromebox-guest ./cmd/chromebox-guest
[[ -f "$HOME/chromebox-guest" ]] || { echo "copy bin/chromebox-guest to ~/chromebox-guest first" >&2; exit 1; }
sudo install -m 0755 "$HOME/chromebox-guest" "$ROOT/usr/local/bin/chromebox-guest"
sudo tee "$ROOT/etc/systemd/system/chromebox-guest.service" >/dev/null <<'EOF'
[Unit]
Description=chromebox guest agent (vsock)

[Service]
ExecStart=/usr/local/bin/chromebox-guest
Restart=always
StandardOutput=file:/dev/console
StandardError=file:/dev/console

[Install]
WantedBy=multi-user.target
EOF
for u in chrome chrome-bridge chrome-ready chromebox-guest; do
  sudo ln -sf "/etc/systemd/system/$u.service" "$ROOT/etc/systemd/system/multi-user.target.wants/$u.service"
done

# 5. Pack the directory into an ext4 image.
cleanup
sudo rm -f "$OUT.tmp"
truncate -s "$IMAGE_SIZE" "$OUT.tmp"
sudo mkfs.ext4 -q -d "$ROOT" -F "$OUT.tmp"
sudo chown "$USER": "$OUT.tmp"
mv "$OUT.tmp" "$OUT"
ls -lsh "$OUT"
