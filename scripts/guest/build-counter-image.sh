#!/usr/bin/env bash
# Build the counter golden image: base-rootfs.ext4 plus a systemd service that
# counts forever. Runs on a node, once. Every counter VM boots from a clone of it.
set -euo pipefail
IMAGES=/var/lib/chromebox/images
BASE=$IMAGES/base-rootfs.ext4
OUT=$IMAGES/counter.ext4

# Start from a copy of the base so the base stays untouched for other images.
cp --sparse=always --reflink=auto "$BASE" "$OUT.tmp"

# Mount the image like a USB stick, write the two files, unmount.
mnt=$(mktemp -d)
sudo mount -o loop "$OUT.tmp" "$mnt"
trap 'sudo umount "$mnt" 2>/dev/null; rmdir "$mnt"' EXIT

# The count lives in a shell variable, i.e. in guest RAM. After a snapshot is
# restored it must continue from there; a fresh boot would start again at 1.
sudo tee "$mnt/usr/local/bin/counter.sh" >/dev/null <<'EOF'
#!/bin/sh
mkdir -p /var/lib/counter
n=0
while true; do
  n=$((n + 1))
  echo "count $n"
  echo "$n" >> /var/lib/counter/count.log
  sleep 0.1
done
EOF
sudo chmod 0755 "$mnt/usr/local/bin/counter.sh"

# stdout goes to /dev/console, the kernel console, which is the serial port and
# so Firecracker's stdout on the host (the VM's serial.log). Not /dev/ttyS0: the
# login prompt takes over ttyS0 at boot and hangs up every other writer, which
# silently killed the counter after two numbers.
sudo tee "$mnt/etc/systemd/system/counter.service" >/dev/null <<'EOF'
[Unit]
Description=chromebox counter
After=local-fs.target

[Service]
ExecStart=/usr/local/bin/counter.sh
StandardOutput=file:/dev/console

[Install]
WantedBy=multi-user.target
EOF
# What `systemctl enable` would do: link the service into the boot target.
sudo ln -sf /etc/systemd/system/counter.service \
  "$mnt/etc/systemd/system/multi-user.target.wants/counter.service"

sudo umount "$mnt"
trap - EXIT
rmdir "$mnt"
mv "$OUT.tmp" "$OUT"
echo "built $OUT"
