// Package vm manages microVMs on one node: their files, their Firecracker
// process, and their lifecycle.
package vm

import "path/filepath"

// StateDir is identical on every node. A snapshot records the absolute paths
// of the files the VM used, so a restore on another node needs the same paths.
const StateDir = "/var/lib/chromebox"

var KernelPath = filepath.Join(StateDir, "kernels", "vmlinux")

// ImagePath is a golden image: a rootfs with one app baked in, e.g. "counter".
func ImagePath(name string) string {
	return filepath.Join(StateDir, "images", name+".ext4")
}

// Paths locates one VM's files. The directory is the VM's only state: the CLI
// keeps nothing in memory between runs.
type Paths struct {
	Dir string
}

func PathsFor(id string) Paths {
	return Paths{Dir: filepath.Join(StateDir, "vms", id)}
}

func (p Paths) Socket() string    { return filepath.Join(p.Dir, "fc.sock") }
func (p Paths) PIDFile() string   { return filepath.Join(p.Dir, "fc.pid") }
func (p Paths) SerialLog() string { return filepath.Join(p.Dir, "serial.log") }
func (p Paths) Rootfs() string    { return filepath.Join(p.Dir, "rootfs.ext4") }

// DiskRef is a reflink clone of the disk taken while the VM still runs, just
// before a migration: its changes against the base travel before the pause,
// and only the blocks written after it travel during the pause.
func (p Paths) DiskRef() string { return filepath.Join(p.Dir, "ref.ext4") }

// VsockSock is the Unix socket through which the host reaches the guest's
// vsock ports (see package guestctl).
func (p Paths) VsockSock() string { return filepath.Join(p.Dir, "v.sock") }

// BaseFile records which base snapshot a VM was cloned from. The VM's disk is a
// copy-on-write clone of that base's disk, which is what lets a migration send
// only the blocks the VM has written.
func (p Paths) BaseFile() string { return filepath.Join(p.Dir, "base") }
func (p Paths) Timings() string  { return filepath.Join(p.Dir, "timings.json") }

// NetFile holds the VM's network index. A snapshot keeps a copy (SnapNet), so
// the network identity travels with the snapshot to another node.
func (p Paths) NetFile() string { return filepath.Join(p.Dir, "net") }

// ImageFile holds the name of the golden image the VM was made from.
func (p Paths) ImageFile() string { return filepath.Join(p.Dir, "image") }

// ShmDir is where snapshot memory lives: tmpfs, i.e. RAM. A migration's memory
// file exists only for the length of the move; writing it to disk would make
// the pause wait for the disk (an EBS volume sustains about 125 MB/s, so 1 GiB
// takes 8 s once the kernel starts throttling writers), while RAM takes
// well under a second. Firecracker maps the file instead of reading it, so a
// restored VM runs directly on those pages.
const ShmDir = "/dev/shm/chromebox"

// MemDir holds the memory file and device state of the VM's snapshot.
func (p Paths) MemDir() string { return filepath.Join(ShmDir, filepath.Base(p.Dir)) }

// A snapshot is three files: the two Firecracker writes (device state and
// guest RAM, in MemDir) plus a clone of the disk taken while paused (in
// SnapDir). Firecracker does not snapshot the disk, so we do.
func (p Paths) SnapDir() string    { return filepath.Join(p.Dir, "snap") }
func (p Paths) Vmstate() string    { return filepath.Join(p.MemDir(), "vmstate") }
func (p Paths) Mem() string        { return filepath.Join(p.MemDir(), "mem") }
func (p Paths) SnapNet() string    { return filepath.Join(p.SnapDir(), "net") }
func (p Paths) SnapImage() string  { return filepath.Join(p.SnapDir(), "image") }
func (p Paths) SnapBase() string   { return filepath.Join(p.SnapDir(), "base") }
func (p Paths) SnapRootfs() string { return filepath.Join(p.SnapDir(), "rootfs.ext4") }
