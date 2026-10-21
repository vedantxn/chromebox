// Package base builds and uses base snapshots: a snapshot of a browser VM that
// has fully started and is idle, kept so that a new VM can be made by restoring
// it (tens of milliseconds) instead of booting (seconds). Every VM made from a
// base starts with the base's memory, so untouched pages stay shared between
// them until one writes.
package base

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/guestctl"
	"github.com/vedantxn/chromebox/internal/netsetup"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

// Dir holds bases on disk; ShmDir holds the memory of the ones in use, in RAM.
const (
	Dir    = vm.StateDir + "/bases"
	ShmDir = vm.ShmDir + "/bases"
	// Index is the network identity the base VM was built with. It is a
	// reserved address that no real VM uses; clones replace it.
	Index = netsetup.MaxIndex
)

// Manifest describes a stored base.
type Manifest struct {
	Name       string    `json:"name"`
	Image      string    `json:"image"`
	VCPUs      int       `json:"vcpus"`
	MemMiB     int       `json:"mem_mib"`
	DiskSHA256 string    `json:"disk_sha256"`
	BuiltAt    time.Time `json:"built_at"`
}

func dir(name string) string      { return filepath.Join(Dir, name) }
func shm(name string) string      { return filepath.Join(ShmDir, name) }
func Disk(name string) string     { return filepath.Join(dir(name), "disk.ext4") }
func manifest(name string) string { return filepath.Join(dir(name), "manifest.json") }

// ShmMem and ShmState are the memory file and device state a clone loads.
func ShmMem(name string) string   { return filepath.Join(shm(name), "mem") }
func ShmState(name string) string { return filepath.Join(shm(name), "vmstate") }

// Read returns a stored base's manifest.
func Read(name string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(manifest(name))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// Build boots a VM from image, waits until the app is up and has settled, takes
// a snapshot, and stores it as base name.
func Build(ctx context.Context, name, image string, settle time.Duration, sw *timing.Stopwatch) error {
	img, err := vm.LookupImage(image)
	if err != nil {
		return err
	}
	p := vm.PathsFor("base-" + name)
	_ = vm.Kill(p)
	_ = os.RemoveAll(p.Dir)
	_ = os.RemoveAll(p.MemDir())

	if err := vm.Create(p, image); err != nil {
		return err
	}
	nn, err := vm.AttachNet(p, Index)
	if err != nil {
		return err
	}
	c, err := vm.StartProcess(ctx, p)
	if err != nil {
		return err
	}
	defer func() { _ = vm.Kill(p); _ = os.RemoveAll(p.Dir); _ = os.RemoveAll(p.MemDir()) }()
	if err := vm.Configure(ctx, c, p, vm.Spec{VCPUs: img.VCPUs, MemMiB: img.MemMiB, Net: nn, Vsock: img.Vsock}); err != nil {
		return err
	}
	if err := c.StartInstance(ctx); err != nil {
		return err
	}
	if _, err := vm.WaitForMatch(ctx, p, img.Ready, 0, 60*time.Second); err != nil {
		return err
	}
	sw.Lap("base VM booted")
	// Run the identity command once, on the base's own address, so the guest has
	// already loaded `ip` and friends into its page cache when the snapshot is
	// taken. Otherwise every clone's first call reads them from disk.
	if err := guestctl.SetIdentity(ctx, p.VsockSock(), nn.GuestIP.String()+"/30", nn.TapIP.String(), time.Now()); err != nil {
		return err
	}
	time.Sleep(settle) // let startup work (and the guest's own caches) finish before freezing it
	if err := vm.Snapshot(ctx, c, p, vm.SnapshotOptions{Sync: false}, nil); err != nil {
		return err
	}
	sw.Lap("snapshot taken")

	// Store: device state and memory on disk (and in RAM for use), the disk as
	// a clone of the snapshot's disk.
	if err := os.MkdirAll(dir(name), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(shm(name), 0o755); err != nil {
		return err
	}
	if err := vm.CloneFile(p.SnapRootfs(), Disk(name)); err != nil {
		return err
	}
	for _, f := range []string{"vmstate", "mem"} {
		if err := copyFile(filepath.Join(p.MemDir(), f), filepath.Join(dir(name), f)); err != nil {
			return err
		}
		if err := os.Rename(filepath.Join(p.MemDir(), f), filepath.Join(shm(name), f)); err != nil {
			return err
		}
	}
	sum, err := hashFile(Disk(name))
	if err != nil {
		return err
	}
	sw.Lap("stored")
	b, _ := json.MarshalIndent(Manifest{Name: name, Image: image, VCPUs: img.VCPUs, MemMiB: img.MemMiB,
		DiskSHA256: sum, BuiltAt: time.Now().UTC()}, "", "  ")
	return os.WriteFile(manifest(name), append(b, '\n'), 0o644)
}

// Load puts a stored base's memory and device state into RAM, where clones map
// them. It does nothing if they are already there.
func Load(name string) error {
	if _, err := Read(name); err != nil {
		return fmt.Errorf("base %q: %w", name, err)
	}
	if err := os.MkdirAll(shm(name), 0o755); err != nil {
		return err
	}
	for _, f := range []string{"vmstate", "mem"} {
		dst := filepath.Join(shm(name), f)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyFile(filepath.Join(dir(name), f), dst); err != nil {
			return err
		}
	}
	return nil
}

// Clone makes VM p from base name, with network index netIndex, and returns
// when its browser answers. The steps, and why they are in this order:
//
//  1. the disk is a copy-on-write clone of the base's disk (a millisecond);
//  2. the TAP device is created while Firecracker starts, since both take time
//     and neither needs the other;
//  3. the snapshot is loaded with the VM's own TAP name and vsock socket, which
//     the snapshot would otherwise take from the VM it was made from;
//  4. the guest, still believing it is the base VM, is told its real address
//     and the time over vsock;
//  5. the browser's endpoint is asked for its version, through the TAP, as
//     proof that the whole path works.
func Clone(ctx context.Context, p vm.Paths, name string, netIndex int, sw *timing.Stopwatch) error {
	m, err := Read(name)
	if err != nil {
		return fmt.Errorf("base %q: %w", name, err)
	}
	if err := os.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	if err := vm.CloneFile(Disk(name), p.Rootfs()); err != nil {
		return err
	}
	for f, v := range map[string]string{p.ImageFile(): m.Image, p.BaseFile(): name, p.NetFile(): strconv.Itoa(netIndex)} {
		if err := os.WriteFile(f, []byte(v+"\n"), 0o644); err != nil {
			return err
		}
	}
	nn, err := netsetup.ForIndex(netIndex)
	if err != nil {
		return err
	}
	sw.Lap("disk cloned")

	tapDone := make(chan error, 1)
	go func() { tapDone <- nn.EnsureTap(os.Getuid(), os.Getgid()) }()
	c, err := vm.StartProcess(ctx, p)
	if err != nil {
		return err
	}
	if err := <-tapDone; err != nil {
		return err
	}
	sw.Lap("tap + firecracker ready")

	if err := c.LoadSnapshot(ctx, firecracker.SnapshotLoad{
		SnapshotPath:     ShmState(name),
		MemBackend:       firecracker.MemBackend{BackendType: "File", BackendPath: ShmMem(name)},
		ResumeVM:         true,
		NetworkOverrides: []firecracker.NetworkOverride{{IfaceID: "eth0", HostDevName: nn.TapName}},
		VsockOverride:    &firecracker.VsockOverride{UDSPath: p.VsockSock()},
	}); err != nil {
		return err
	}
	sw.Lap("snapshot loaded")

	if err := guestctl.SetIdentity(ctx, p.VsockSock(), nn.GuestIP.String()+"/30", nn.TapIP.String(), time.Now()); err != nil {
		return err
	}
	sw.Lap("identity set")

	if _, err := vm.WaitRestored(ctx, p, 0, 10*time.Second); err != nil {
		return err
	}
	sw.Lap("browser answering")
	return nil
}

func copyFile(src, dst string) error {
	out, err := exec.Command("cp", "--sparse=always", "--reflink=auto", src, dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy %s: %w: %s", src, err, out)
	}
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
