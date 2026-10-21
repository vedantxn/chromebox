package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/netsetup"
	"github.com/vedantxn/chromebox/internal/timing"
)

// Spec is the shape of a new VM.
type Spec struct {
	VCPUs  int
	MemMiB int
	Net    *netsetup.Net // nil: the VM has no network
	Vsock  bool          // add a vsock device for the guest agent
}

// bootArgs: console=ttyS0 sends kernel output to the serial port (Firecracker's
// stdout); reboot=k and panic=1 make a reboot or a kernel crash end the VM
// instead of hanging it.
const bootArgs = "console=ttyS0 reboot=k panic=1"

// Create makes the VM's directory and gives it a private, writable clone of
// the golden image. The clone is copy-on-write, so it is instant.
func Create(p Paths, image string) error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.ImageFile(), []byte(image+"\n"), 0o644); err != nil {
		return err
	}
	return clone(ImagePath(image), p.Rootfs())
}

// CloneFile copies a file using the filesystem's copy-on-write clone (reflink):
// the new file shares blocks with the old one until either is written, so a
// 1 GiB disk clones in about a millisecond. dst is overwritten if it exists.
func CloneFile(src, dst string) error { return clone(src, dst) }

func clone(src, dst string) error {
	out, err := exec.Command("cp", "--sparse=always", "--reflink=auto", src, dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone %s: %w: %s", src, err, out)
	}
	return nil
}

// Configure describes the machine to a fresh Firecracker process: kernel,
// disk, CPU and memory. Nothing runs until the caller sends InstanceStart.
func Configure(ctx context.Context, c *firecracker.Client, p Paths, s Spec) error {
	args := bootArgs
	if s.Net != nil {
		args += " " + s.Net.BootArgs()
	}
	if err := c.PutBootSource(ctx, firecracker.BootSource{
		KernelImagePath: KernelPath,
		BootArgs:        args,
	}); err != nil {
		return err
	}
	if s.Net != nil {
		if err := c.PutNetworkInterface(ctx, firecracker.NetworkInterface{
			IfaceID:     "eth0",
			GuestMAC:    s.Net.GuestMAC,
			HostDevName: s.Net.TapName,
		}); err != nil {
			return err
		}
	}
	if err := c.PutDrive(ctx, firecracker.Drive{
		DriveID:      "rootfs",
		PathOnHost:   DrivePath, // the VM's own disk, bound there by StartProcess
		IsRootDevice: true,
	}); err != nil {
		return err
	}
	if s.Vsock {
		if err := c.PutVsock(ctx, firecracker.Vsock{GuestCID: 3, UDSPath: p.VsockSock()}); err != nil {
			return err
		}
	}
	return c.PutMachineConfig(ctx, firecracker.MachineConfig{
		VCPUCount:  s.VCPUs,
		MemSizeMiB: s.MemMiB,
	})
}

// SnapshotOptions tunes Snapshot.
type SnapshotOptions struct {
	// Sync makes Firecracker fsync the snapshot files before it replies. That
	// is durable across a host crash but waits for the disk; without it the
	// files are in the host page cache, which is all a same-host restore or an
	// immediate copy to another node needs.
	Sync bool
}

// Snapshot pauses the VM and saves everything needed to bring it back into
// p.SnapDir(): vmstate and mem from Firecracker, and a clone of the disk.
// The VM is left paused; the caller decides whether to resume it (checkpoint)
// or kill it (migration).
//
// Order matters. The disk is cloned after Firecracker has written the snapshot
// (it flushes the disk then) and while the guest is paused, so no write can
// land between the memory image and the disk image.
func Snapshot(ctx context.Context, c *firecracker.Client, p Paths, o SnapshotOptions, sw *timing.Stopwatch) error {
	for _, d := range []string{p.SnapDir(), p.MemDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := c.PauseVM(ctx); err != nil {
		return err
	}
	sw.Lap("paused")
	if err := c.CreateSnapshot(ctx, firecracker.SnapshotCreate{
		SnapshotPath:      p.Vmstate(),
		MemFilePath:       p.Mem(),
		SnapshotType:      "Full",
		SyncSnapshotFiles: &o.Sync,
	}); err != nil {
		return err
	}
	sw.Lap("snapshot written")
	if err := clone(p.Rootfs(), p.SnapRootfs()); err != nil {
		return err
	}
	sw.Lap("disk cloned")
	// The network identity and the image name are part of the snapshot:
	// whoever restores it must recreate the same TAP and MACs, and must know
	// how to tell that the guest is running again.
	for _, f := range [][2]string{{p.NetFile(), p.SnapNet()}, {p.ImageFile(), p.SnapImage()}} {
		if b, err := os.ReadFile(f[0]); err == nil {
			if err := os.WriteFile(f[1], b, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// Restore brings the VM in p.SnapDir() back to life, running. Any process
// still holding this VM is killed first.
//
// The disk file at its original path is replaced by the snapshot's clone,
// because the restored guest believes its disk is exactly as it was when it
// was paused; a disk that moved on (or lagged behind) would contradict the
// guest's memory. Then a fresh Firecracker process loads the snapshot. It must
// not be configured first: the snapshot already contains the configuration.
func Restore(ctx context.Context, p Paths, sw *timing.Stopwatch) (*firecracker.Client, error) {
	_ = Kill(p) // a missing pid file just means nothing is running
	if err := clone(p.SnapRootfs(), p.Rootfs()); err != nil {
		return nil, err
	}
	sw.Lap("disk restored")
	// The restored guest expects its TAP to exist, with the same name and MAC.
	nn, err := NetOf(p)
	if err != nil {
		return nil, err
	}
	if nn != nil {
		if err := nn.EnsureTap(os.Getuid(), os.Getgid()); err != nil {
			return nil, err
		}
		sw.Lap("tap ready")
	}
	c, err := StartProcess(ctx, p)
	if err != nil {
		return nil, err
	}
	sw.Lap("firecracker API ready")
	load := firecracker.SnapshotLoad{
		SnapshotPath: p.Vmstate(),
		MemBackend:   firecracker.MemBackend{BackendType: "File", BackendPath: p.Mem()},
		ResumeVM:     true,
		// Off by default: see docs/learn/m2.md (C5) for why.
		ClockRealtime: os.Getenv("CHROMEBOX_CLOCK_REALTIME") != "",
	}
	if _, img, err := ImageOf(p); err == nil && img.Vsock {
		load.VsockOverride = &firecracker.VsockOverride{UDSPath: p.VsockSock()}
	}
	if err := c.LoadSnapshot(ctx, load); err != nil {
		return nil, err
	}
	sw.Lap("snapshot loaded")
	return c, nil
}

// AttachNet gives the VM network index n: it creates the TAP device and
// records the index in the VM's directory. Call it before Configure.
func AttachNet(p Paths, n int) (*netsetup.Net, error) {
	nn, err := netsetup.ForIndex(n)
	if err != nil {
		return nil, err
	}
	if err := nn.EnsureTap(os.Getuid(), os.Getgid()); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p.NetFile(), []byte(strconv.Itoa(n)+"\n"), 0o644); err != nil {
		return nil, err
	}
	return &nn, nil
}

// NetOf returns the VM's network identity, read from its directory or, on a
// node that only holds a received snapshot, from the snapshot. nil means the VM
// has no network.
func NetOf(p Paths) (*netsetup.Net, error) {
	for _, f := range []string{p.NetFile(), p.SnapNet()} {
		b, err := os.ReadFile(f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, fmt.Errorf("bad net file %s: %w", f, err)
		}
		nn, err := netsetup.ForIndex(n)
		return &nn, err
	}
	return nil, nil
}
