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
func (p Paths) Timings() string   { return filepath.Join(p.Dir, "timings.json") }

// NetFile holds the VM's network index. A snapshot keeps a copy (SnapNet), so
// the network identity travels with the snapshot to another node.
func (p Paths) NetFile() string { return filepath.Join(p.Dir, "net") }

// ImageFile holds the name of the golden image the VM was made from.
func (p Paths) ImageFile() string { return filepath.Join(p.Dir, "image") }

// A snapshot is three files kept together in snap/: the two Firecracker writes
// (device state and guest RAM) plus a clone of the disk taken while paused.
// Firecracker does not snapshot the disk, so we do.
func (p Paths) SnapDir() string    { return filepath.Join(p.Dir, "snap") }
func (p Paths) Vmstate() string    { return filepath.Join(p.SnapDir(), "vmstate") }
func (p Paths) Mem() string        { return filepath.Join(p.SnapDir(), "mem") }
func (p Paths) SnapNet() string    { return filepath.Join(p.SnapDir(), "net") }
func (p Paths) SnapImage() string  { return filepath.Join(p.SnapDir(), "image") }
func (p Paths) SnapRootfs() string { return filepath.Join(p.SnapDir(), "rootfs.ext4") }
