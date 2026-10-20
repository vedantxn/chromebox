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

// Create makes the VM's directory and gives it a private, writable clone of
// the golden image. The clone is copy-on-write, so it is instant.
func Create(p Paths, image string) error {
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	out, err := exec.Command("cp", "--sparse=always", "--reflink=auto", ImagePath(image), p.Rootfs()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy rootfs: %w: %s", err, out)
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
