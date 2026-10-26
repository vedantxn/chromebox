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
	"strings"
	"syscall"
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
	// Default is the base new browsers are made from. chrome-v3's guest agent
	// drops the DevTools connections a restored copy wakes up with, when the
	// host asks (only for restores of running browsers). chrome-v1 and
	// chrome-v2 stay on every node: stored sessions and profiles keep their
	// disks as the blocks that differ from them.
	Default = "chrome-v3"
)

// Manifest describes a stored base.
type Manifest struct {
	Name       string    `json:"name"`
	Image      string    `json:"image"`
	VCPUs      int       `json:"vcpus"`
	MemMiB     int       `json:"mem_mib"`
	DiskSHA256 string    `json:"disk_sha256"`
	BuiltAt    time.Time `json:"built_at"`
	// ForkOf names the VM a fork image was taken from (M6). A fork image is a
	// base made on the fly from a running browser, kept only while VMs use it.
	ForkOf string `json:"fork_of,omitempty"`
	// DiskBase is the regular base this disk descends from (reflink by
	// reflink), which every node holds: another node can rebuild the disk
	// from its own copy of it plus the blocks that differ.
	DiskBase string `json:"disk_base,omitempty"`
}

// RootBase is the regular base a VM made from base name descends from: the
// base itself, or for a fork image the base its disk was cloned from. Every
// node holds the root bases, so a snapshot's disk can be stored as the blocks
// that differ from one.
func RootBase(name string) string { return diskBase(name) }

// diskBase is the regular base a VM made from base name descends from.
func diskBase(name string) string {
	if m, err := Read(name); err == nil && m.DiskBase != "" {
		return m.DiskBase
	}
	return name
}

// IsFork says whether a base name is a fork image.
func IsFork(name string) bool { return strings.HasPrefix(name, "fork-") }

// HeldMiB is how much RAM the base holds now: its memory file in tmpfs, by
// the pages really allocated (holes cost nothing), or, if not loaded yet, its
// full size, since it will be loaded as soon as a VM needs it.
func HeldMiB(name string, m Manifest) int {
	var st syscall.Stat_t
	if err := syscall.Stat(ShmMem(name), &st); err != nil {
		return m.MemMiB
	}
	return int(st.Blocks * 512 >> 20)
}

// Delete removes a fork image. VMs made from it keep running: they map its
// memory, so the pages live until the last of them exits.
func Delete(name string) error {
	if !IsFork(name) {
		return fmt.Errorf("%s is not a fork image", name)
	}
	_ = os.RemoveAll(shm(name))
	return os.RemoveAll(dir(name))
}

// Loaded says whether base name's memory is in RAM, where clones map it.
func Loaded(name string) bool {
	_, err := os.Stat(ShmMem(name))
	return err == nil
}

// Unload frees the RAM a regular base holds: its memory and device state leave
// tmpfs and stay on disk, from where Load brings them back when a VM is next
// made from it. Only for a base no running VM was made from. Fork images live
// only in RAM, so they are not unloaded but deleted (Delete).
func Unload(name string) error {
	if IsFork(name) {
		return fmt.Errorf("%s is a fork image: delete it instead", name)
	}
	for _, f := range []string{"vmstate", "mem"} {
		if _, err := os.Stat(filepath.Join(dir(name), f)); err != nil {
			return fmt.Errorf("base %s: no copy on disk, keeping it in RAM: %w", name, err)
		}
	}
	return os.RemoveAll(shm(name))
}

// Fork turns running VM p into fork image name, and returns how much of its
// memory was zero pages (punched out of the image after the VM resumed).
//
// The VM is paused only while Firecracker writes its state and memory (into a
// file allocated beforehand, as in a migration) and its disk is cloned: about
// as long as a migration's pause. Then it runs on, unaware. New VMs are made
// from the image exactly as from a base (Clone): they map its memory
// copy-on-write and are told their own address over vsock.
func Fork(ctx context.Context, c *firecracker.Client, p vm.Paths, name string, sw *timing.Stopwatch) (int64, error) {
	if !IsFork(name) {
		return 0, fmt.Errorf("fork image names start with fork-: %q", name)
	}
	imgName, img, err := vm.ImageOf(p)
	if err != nil {
		return 0, err
	}
	for _, d := range []string{dir(name), shm(name)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return 0, err
		}
	}
	// The parent is jailed: it can write only into its own directories. The
	// snapshot goes into its memory dir, then is renamed into the image (same
	// tmpfs, so the rename moves nothing).
	tmpState, tmpMem := filepath.Join(p.MemDir(), "fork-vmstate"), filepath.Join(p.MemDir(), "fork-mem")
	if err := vm.Preallocate(tmpMem, int64(img.MemMiB)<<20); err != nil {
		return 0, err
	}
	sw.Lap("image prepared")
	// Map all of the parent's memory while it still runs, so the snapshot
	// doesn't fault it in during the pause. Only an optimization: on failure
	// the pause is just longer.
	if err := vm.Prefault(p, int64(img.MemMiB)<<20); err != nil {
		fmt.Fprintf(os.Stderr, "fork %s: prefault: %v\n", name, err)
	}
	sw.Lap("parent memory mapped")

	if err := c.PauseVM(ctx); err != nil {
		return 0, err
	}
	sw.Lap("paused")
	nosync := false
	err = c.CreateSnapshot(ctx, firecracker.SnapshotCreate{SnapshotPath: vm.JailMemDir + "/fork-vmstate", MemFilePath: vm.JailMemDir + "/fork-mem",
		SnapshotType: "Full", SyncSnapshotFiles: &nosync})
	sw.Lap("snapshot written")
	if err == nil {
		err = vm.CloneFile(p.Rootfs(), Disk(name)) // while paused: the disk matches the memory
	}
	sw.Lap("disk cloned")
	if rerr := c.ResumeVM(context.Background()); err == nil {
		err = rerr
	}
	if err == nil {
		err = os.Rename(tmpState, ShmState(name))
	}
	if err == nil {
		err = os.Rename(tmpMem, ShmMem(name))
	}
	if err != nil {
		_ = os.Remove(tmpState)
		_ = os.Remove(tmpMem)
		_ = Delete(name)
		return 0, err
	}
	sw.Lap("resumed")

	freed, err := vm.PunchZeros(ShmMem(name))
	if err != nil {
		_ = Delete(name)
		return 0, err
	}
	sw.Lap("zero pages punched out")
	m := Manifest{Name: name, Image: imgName, VCPUs: img.VCPUs, MemMiB: img.MemMiB,
		BuiltAt: time.Now().UTC(), ForkOf: filepath.Base(p.Dir), DiskBase: diskBase(vm.BaseOf(p))}
	return freed, writeManifest(m)
}

func writeManifest(m Manifest) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(manifest(m.Name), append(b, '\n'), 0o644)
}

// Install receives a fork image made on another node: its manifest and
// device state, its disk as the blocks that differ from m.DiskBase (which this
// node holds), and its memory as runs of non-zero pages (pageserver.WriteRuns
// format), written into a sparse file in RAM: zero pages stay holes. The
// manifest is written last, so a half-received image is never used.
func Install(m Manifest, vmstate []byte, disk func(path string) error, memory func(path string) error) error {
	if !IsFork(m.Name) {
		return fmt.Errorf("only fork images travel between nodes: %q", m.Name)
	}
	if _, err := Read(m.DiskBase); err != nil {
		return fmt.Errorf("this node lacks base %s that %s descends from", m.DiskBase, m.Name)
	}
	for _, d := range []string{dir(m.Name), shm(m.Name)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	fail := func(err error) error { _ = Delete(m.Name); return err }
	if err := vm.CloneFile(Disk(m.DiskBase), Disk(m.Name)); err != nil {
		return fail(err)
	}
	if err := disk(Disk(m.Name)); err != nil {
		return fail(fmt.Errorf("disk: %w", err))
	}
	if err := os.WriteFile(ShmState(m.Name), vmstate, 0o644); err != nil {
		return fail(err)
	}
	if err := memory(ShmMem(m.Name)); err != nil {
		return fail(fmt.Errorf("memory: %w", err))
	}
	return writeManifest(m)
}

// ImageFiles is where a fork image's parts are, for sending it elsewhere.
func ImageFiles(name string) (vmstate, mem, disk string) {
	return ShmState(name), ShmMem(name), Disk(name)
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
	if err := guestctl.SetIdentity(ctx, p.VsockSock(), nn.GuestIP.String()+"/30", nn.TapIP.String(), time.Now(), false); err != nil {
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
//
// With a slot (a jailed Firecracker started ahead of time by PrepareSlot for
// this base), steps 1 and 2's process start are already done: the slot's
// directories are renamed to the VM's, and the VM sees no difference, since
// inside its jail its directories are always /vm and /mem.
func Clone(ctx context.Context, p vm.Paths, name string, netIndex int, sw *timing.Stopwatch, slot ...vm.Paths) error {
	m, err := Read(name)
	if err != nil {
		return fmt.Errorf("base %q: %w", name, err)
	}
	nn, err := netsetup.ForIndex(netIndex)
	if err != nil {
		return err
	}
	var c *firecracker.Client
	if len(slot) > 0 {
		if err := os.Rename(slot[0].Dir, p.Dir); err != nil {
			return err
		}
		if err := os.Rename(slot[0].MemDir(), p.MemDir()); err != nil {
			return err
		}
		c = firecracker.New(p.Socket())
		sw.Lap("pool slot claimed")
	} else {
		if err := os.MkdirAll(p.Dir, 0o755); err != nil {
			return err
		}
		if err := vm.CloneFile(Disk(name), p.Rootfs()); err != nil {
			return err
		}
		sw.Lap("disk cloned")
	}
	for f, v := range map[string]string{p.ImageFile(): m.Image, p.BaseFile(): name, p.NetFile(): strconv.Itoa(netIndex)} {
		if err := os.WriteFile(f, []byte(v+"\n"), 0o644); err != nil {
			return err
		}
	}
	if c == nil {
		tapDone := make(chan error, 1)
		go func() { tapDone <- nn.EnsureTap(os.Getuid(), os.Getgid()) }()
		if c, err = vm.StartProcess(ctx, p, shm(name)); err != nil { // the jailed VM sees its base read-only
			return err
		}
		if err := <-tapDone; err != nil {
			return err
		}
		sw.Lap("tap + firecracker ready")
	} else {
		if err := nn.EnsureTap(os.Getuid(), os.Getgid()); err != nil {
			return err
		}
		sw.Lap("tap ready")
	}

	mem := firecracker.MemBackend{BackendType: "File", BackendPath: ShmMem(name)}
	// CHROMEBOX_UFFD (M5, experiment): serve the memory through our own
	// page-fault handler instead of letting Firecracker map the file.
	if v, ok := os.LookupEnv("CHROMEBOX_UFFD"); ok {
		args := []string{"file=" + ShmMem(name)}
		if v != "" {
			args = append(args, v)
		}
		if err := vm.StartUffd(p, args...); err != nil {
			return err
		}
		mem = firecracker.MemBackend{BackendType: "Uffd", BackendPath: vm.JailUffd}
		sw.Lap("uffd handler ready")
	}
	if err := c.LoadSnapshot(ctx, firecracker.SnapshotLoad{
		SnapshotPath:     ShmState(name),
		MemBackend:       mem,
		ResumeVM:         true,
		NetworkOverrides: []firecracker.NetworkOverride{{IfaceID: "eth0", HostDevName: nn.TapName}},
		VsockOverride:    &firecracker.VsockOverride{UDSPath: vm.JailVsock},
	}); err != nil {
		return err
	}
	sw.Lap("snapshot loaded")

	// A fork image is a running browser, perhaps with clients connected; a
	// regular base never had any.
	if err := guestctl.SetIdentity(ctx, p.VsockSock(), nn.GuestIP.String()+"/30", nn.TapIP.String(), time.Now(), IsFork(name)); err != nil {
		return err
	}
	sw.Lap("identity set")

	if _, err := vm.WaitRestored(ctx, p, 0, 10*time.Second); err != nil {
		return err
	}
	sw.Lap("browser answering")
	if IsFork(name) && os.Getenv("CHROMEBOX_FORK_KEEP_PAGES") == "" {
		img, err := vm.LookupImage(m.Image)
		if err != nil {
			return err
		}
		if _, err := freshTabs(ctx, fmt.Sprintf("%s:%d", nn.GuestIP, img.CDPPort)); err != nil {
			return fmt.Errorf("fresh tabs: %w", err)
		}
		sw.Lap("open tabs moved to fresh renderers")
	}
	return nil
}

// PrepareSlot starts a pool slot for base name: the base's disk cloned and a
// jailed Firecracker waiting, with the base visible to it read-only. That is
// the part of a create that does not depend on the VM's id or address (~70 ms
// of process and jail start), done before anyone asks. Clone then claims it.
func PrepareSlot(ctx context.Context, slot vm.Paths, name string) error {
	m, err := Read(name)
	if err != nil {
		return fmt.Errorf("base %q: %w", name, err)
	}
	if err := os.MkdirAll(slot.Dir, 0o755); err != nil {
		return err
	}
	if err := vm.CloneFile(Disk(name), slot.Rootfs()); err != nil {
		return err
	}
	// The image sets the jail's memory limit.
	if err := os.WriteFile(slot.ImageFile(), []byte(m.Image+"\n"), 0o644); err != nil {
		return err
	}
	_, err = vm.StartProcess(ctx, slot, shm(name))
	return err
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
