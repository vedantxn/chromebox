package vm

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/vedantxn/chromebox/internal/firecracker"
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

// Create makes the VM's directory and gives it a private, writable copy of the
// base rootfs. The copy is sparse, so only blocks in use take disk space.
func Create(p Paths) error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	out, err := exec.Command("cp", "--sparse=always", "--reflink=auto", BaseRootfsPath, p.Rootfs()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy rootfs: %w: %s", err, out)
	}
	return nil
}

// Boot configures a fresh Firecracker process and starts the guest: describe
// the machine first, then start it.
func Boot(ctx context.Context, c *firecracker.Client, p Paths, s Spec) error {
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
	if err := c.PutMachineConfig(ctx, firecracker.MachineConfig{
		VCPUCount:  s.VCPUs,
		MemSizeMiB: s.MemMiB,
	}); err != nil {
		return err
	}
	return c.StartInstance(ctx)
}
