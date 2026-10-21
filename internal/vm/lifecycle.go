package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/timing"
)

// Spec is the shape of a new VM.
type Spec struct {
	VCPUs  int
	MemMiB int
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
	return clone(ImagePath(image), p.Rootfs())
}

// clone copies a file using the filesystem's copy-on-write clone (reflink):
// the new file shares blocks with the old one until either is written, so a
// 1 GiB disk clones in about a millisecond. dst is overwritten if it exists.
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
	if err := c.PutBootSource(ctx, firecracker.BootSource{
		KernelImagePath: KernelPath,
		BootArgs:        bootArgs,
	}); err != nil {
		return err
	}
	if err := c.PutDrive(ctx, firecracker.Drive{
		DriveID:      "rootfs",
		PathOnHost:   p.Rootfs(),
		IsRootDevice: true,
	}); err != nil {
		return err
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
	if err := os.MkdirAll(p.SnapDir(), 0o755); err != nil {
		return err
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
	c, err := StartProcess(ctx, p)
	if err != nil {
		return nil, err
	}
	sw.Lap("firecracker API ready")
	if err := c.LoadSnapshot(ctx, firecracker.SnapshotLoad{
		SnapshotPath: p.Vmstate(),
		MemBackend:   firecracker.MemBackend{BackendType: "File", BackendPath: p.Mem()},
		ResumeVM:     true,
	}); err != nil {
		return nil, err
	}
	sw.Lap("snapshot loaded")
	return c, nil
}
