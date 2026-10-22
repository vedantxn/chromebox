// Package migrate moves a running VM from this node to another: pause it,
// snapshot it, copy the snapshot, restore it there, and only then destroy the
// original.
package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vedantxn/chromebox/internal/blockdiff"
	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/gateway"
	"github.com/vedantxn/chromebox/internal/node"
	"github.com/vedantxn/chromebox/internal/remote"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

// Result describes one finished migration.
type Result struct {
	Laps     *timing.Stopwatch // steps on this node, from the start of migrate
	Downtime time.Duration     // VM paused here -> running again on the target
	// Reachable is how long the VM was unreachable from the network: paused here
	// -> the gateway's route points at the target. Zero without a gateway.
	Reachable time.Duration
	Before    int           // last count printed here before the pause
	After     int           // first count printed on the target after the restore
	Remote    timing.Record // the target's own breakdown of its restore
	// DeltaBytes is how much of the disk had changed since the reference clone
	// and was sent while the VM was paused.
	DeltaBytes int64
}

// Report is what `chromebox accept` prints on the target for the source to read.
type Report struct {
	After int           `json:"after"`
	Node  string        `json:"node"` // the target's name, which names its tunnel on the gateway
	Laps  timing.Record `json:"laps"`
}

// Run migrates VM p to host (an ssh target such as ec2-user@172.31.2.181).
//
// The disk travels in two parts. Before the pause, a reflink clone of it (the
// reference, which cannot change) is copied to the target while the VM keeps
// running: that moves the bulk at no cost in downtime. After the pause, only
// the parts of the disk that differ from the reference are sent, found by
// comparing where the two files' blocks live on the device (package
// blockdiff), not by reading them. The same idea, "copy while it runs, then
// send what changed", is how live migration moves memory.
//
// The order is what makes it safe. The source VM is only destroyed after the
// target has proven, by printing a new count, that it is running. Until that
// moment the source is paused and untouched, so any failure rolls back by
// resuming it.
func Run(ctx context.Context, p vm.Paths, host string) (res *Result, err error) {
	sw := timing.Start()
	r := remote.New(host)
	c := firecracker.New(p.Socket())
	targetStarted, flipped := false, false

	// A VM with a network identity is reached through the gateway, whose
	// route to the VM we must move along with it.
	cfg, err := node.Load()
	if err != nil {
		return nil, err
	}
	nn, err := vm.NetOf(p)
	if err != nil {
		return nil, err
	}
	var gw *remote.Remote
	if cfg != nil && cfg.Gateway != "" && nn != nil {
		gw = remote.New(cfg.Gateway)
	}

	defer func() {
		if err == nil {
			return
		}
		if flipped { // put the route back before resuming the source
			_ = gateway.SetRoute(context.Background(), gw, nn.Block(), cfg.Tunnel())
		}
		if targetStarted {
			_, _ = r.Run(context.Background(), "chromebox", "kill", filepath.Base(p.Dir))
		}
		_, _ = r.Run(context.Background(), "rm", "-rf", p.Dir, p.MemDir())
		_ = os.RemoveAll(p.MemDir())
		_ = c.ResumeVM(context.Background()) // errors if it was never paused; fine
		err = fmt.Errorf("migration rolled back, source resumed: %w", err)
	}()

	// Everything before the pause is preparation and is not downtime.
	if _, err := r.Run(ctx, "true"); err != nil { // opens the shared ssh connection
		return nil, fmt.Errorf("cannot reach %s: %w", host, err)
	}
	sw.Lap("ssh connected")
	if gw != nil {
		if _, err := gw.Run(ctx, "true"); err != nil {
			return nil, fmt.Errorf("cannot reach gateway %s: %w", cfg.Gateway, err)
		}
		sw.Lap("gateway connected")
	}
	if _, err := r.Run(ctx, "mkdir", "-p", p.SnapDir(), p.MemDir()); err != nil {
		return nil, err
	}
	sw.Lap("target prepared")
	// The reference must not change while it is copied, so clone it first and
	// copy the clone; the VM goes on writing to its own file.
	ref := filepath.Join(p.Dir, "rootfs.ref")
	defer os.Remove(ref)
	if err := vm.CloneFile(p.Rootfs(), ref); err != nil {
		return nil, err
	}
	if err := copyDisk(ctx, r, ref, p.SnapRootfs()); err != nil {
		return nil, err
	}
	sw.Lap("disk pre-copied")

	downStart := time.Now()
	if err := vm.Snapshot(ctx, c, p, vm.SnapshotOptions{Sync: false}, sw); err != nil {
		return nil, err
	}
	_, img, err := vm.ImageOf(p)
	if err != nil {
		return nil, err
	}
	before := 0
	if img.Counter {
		if before, err = vm.LastCount(p); err != nil {
			return nil, err
		}
	}

	// Memory and the disk delta are independent, so send them at the same time
	// over the shared connection; the slower one sets the pace.
	deltaBytes, err := CopyState(ctx, r, p, ref, true)
	if err != nil {
		return nil, err
	}
	sw.Lap("memory + disk delta copied")
	if os.Getenv("CHROMEBOX_VERIFY_DISK") != "" { // slow: tests only
		if err := verifyDisk(ctx, r, p); err != nil {
			return nil, err
		}
		sw.Lap("disk verified (test only)")
	}

	targetStarted = true
	out, err := r.Run(ctx, append(passEnv(), "chromebox", "accept", filepath.Base(p.Dir))...)
	if err != nil {
		return nil, err
	}
	var rep Report
	if err := json.Unmarshal(out, &rep); err != nil {
		return nil, fmt.Errorf("bad report from target: %w: %s", err, out)
	}
	down := time.Since(downStart)
	sw.Lap("restored on target")

	// Point the network at the new node. The gateway's route to the VM's
	// address block is the only thing that changes; the VM's address, MACs and
	// TCP state are the same as before.
	var reachable time.Duration
	if gw != nil && os.Getenv("CHROMEBOX_FAULT") != "no-flip" { // no-flip: fault injection
		if rep.Node == "" {
			return nil, fmt.Errorf("target did not report its node name; is %s configured?", "/var/lib/chromebox/node.json")
		}
		if err := gateway.SetRoute(ctx, gw, nn.Block(), "gre-"+rep.Node); err != nil {
			return nil, err
		}
		flipped = true
		reachable = time.Since(downStart)
		sw.Lap("route flipped")
	}

	// The VM resumed from the snapshot if its next count follows the last one
	// printed here. A reboot would print 1; a stale or wrong snapshot would
	// land somewhere else entirely.
	if img.Counter && (rep.After <= before || rep.After > before+5) {
		return nil, fmt.Errorf("target resumed at count %d, expected just after %d", rep.After, before)
	}

	// Verified: the point of no return. Destroy the original.
	if err := vm.Kill(p); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(p.Dir); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(p.MemDir()) // frees the snapshot's RAM
	sw.Lap("source destroyed")

	return &Result{Laps: sw, Downtime: down, Reachable: reachable, Before: before, After: rep.After, Remote: rep.Laps, DeltaBytes: deltaBytes}, nil
}

// copyMemory streams the snapshot's memory and device state (in tmpfs) plus the
// small files in the snapshot directory (the network identity and image name)
// to the same paths on the target: tar here, through the shared ssh connection,
// into tar there. The disk image is excluded; it travels separately.
//
// --sparse writes the runs of zeros as holes, so they cost nothing to send or
// store (a fresh Chrome guest's memory is about 40% all-zero pages). No
// compression: it costs more CPU time than the network takes to move bytes.
func copyMemory(ctx context.Context, r *remote.Remote, p vm.Paths, withMemory bool) error {
	// Relative to /, so both directories fit in one stream and land at the
	// same absolute paths on the target.
	args := []string{"-C", "/", "--sparse", "--exclude=rootfs.ext4"}
	if !withMemory {
		args = append(args, "--exclude=mem") // post-copy: the memory follows later
	}
	args = append(args, "-cf", "-", strings.TrimPrefix(p.SnapDir(), "/"), strings.TrimPrefix(p.MemDir(), "/"))
	send := exec.CommandContext(ctx, "tar", args...)
	recv := r.Command(ctx, "tar", "-C", "/", "-xf", "-")

	pipe, err := send.StdoutPipe()
	if err != nil {
		return err
	}
	recv.Stdin = pipe
	var sendErr, recvErr bytes.Buffer
	send.Stderr, recv.Stderr = &sendErr, &recvErr

	if err := recv.Start(); err != nil {
		return err
	}
	if err := send.Start(); err != nil {
		_ = recv.Process.Kill()
		return err
	}
	// Wait for the sender first: its exit closes the pipe, which lets the
	// receiver see end-of-input and finish.
	sErr := send.Wait()
	rErr := recv.Wait()
	if sErr != nil || rErr != nil {
		return fmt.Errorf("copy memory: send: %v %s; receive: %v %s", sErr, bytes.TrimSpace(sendErr.Bytes()), rErr, bytes.TrimSpace(recvErr.Bytes()))
	}
	return nil
}

// CopyState sends the snapshot (device state, and the memory if withMemory)
// and the disk delta against ref concurrently, and returns the size of the
// delta. The target must already hold a copy of ref at p.SnapRootfs().
func CopyState(ctx context.Context, r *remote.Remote, p vm.Paths, ref string, withMemory bool) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	var delta int64
	go func() { errc <- copyMemory(ctx, r, p, withMemory) }()
	go func() {
		var err error
		delta, err = sendDelta(ctx, r, p, ref)
		errc <- err
	}()
	var first error
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil && first == nil {
			first = err
			cancel() // stop the other transfer; its error is not interesting
		}
	}
	return delta, first
}

// copyDisk copies a whole file with rsync (-S writes runs of zeros as holes).
func copyDisk(ctx context.Context, r *remote.Remote, src, dst string) error {
	args := []string{"-aS", "-e", r.SSHCmd(), src, r.Host + ":" + dst}
	if out, err := exec.CommandContext(ctx, "rsync", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("rsync disk: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// sendDelta makes the target's copy of the disk match this VM's paused disk.
// The target already holds the reference; send only the byte ranges where the
// VM's disk differs from it.
func sendDelta(ctx context.Context, r *remote.Remote, p vm.Paths, ref string) (int64, error) {
	live, err := blockdiff.Extents(p.Rootfs())
	if err != nil {
		return 0, err
	}
	old, err := blockdiff.Extents(ref)
	if err != nil {
		return 0, err
	}
	st, err := os.Stat(p.Rootfs())
	if err != nil {
		return 0, err
	}
	ranges := blockdiff.Diff(live, old, uint64(st.Size()))

	f, err := os.Open(p.Rootfs())
	if err != nil {
		return 0, err
	}
	defer f.Close()

	cmd := r.Command(ctx, "chromebox", "apply-delta", p.SnapRootfs())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	sendErr := blockdiff.Send(stdin, f, ranges)
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		return 0, fmt.Errorf("apply-delta on target: %w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	if sendErr != nil {
		return 0, sendErr
	}
	return int64(blockdiff.Total(ranges)), nil
}

// verifyDisk compares a checksum of the paused disk here with the target's
// copy. It reads both files completely, so it is for tests only.
func verifyDisk(ctx context.Context, r *remote.Remote, p vm.Paths) error {
	local, err := exec.CommandContext(ctx, "sha256sum", p.Rootfs()).Output()
	if err != nil {
		return err
	}
	far, err := r.Run(ctx, "sha256sum", p.SnapRootfs())
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.Fields(local)[0], bytes.Fields(far)[0]) {
		return fmt.Errorf("disk on target differs from the source's")
	}
	return nil
}

// Discard destroys a VM on the target. Benchmarks use it to clean up.
func Discard(ctx context.Context, p vm.Paths, host string) error {
	r := remote.New(host)
	_, _ = r.Run(ctx, "chromebox", "kill", filepath.Base(p.Dir))
	_, err := r.Run(ctx, "rm", "-rf", p.Dir)
	return err
}

// passEnv makes a command run through ssh on the target see the settings used
// for break-it tests (fault injection) and the clock option; ssh itself does
// not forward the environment.
func passEnv() []string {
	args := []string{"env"}
	for _, k := range []string{"CHROMEBOX_FAULT", "CHROMEBOX_CLOCK_REALTIME"} {
		if v := os.Getenv(k); v != "" {
			args = append(args, k+"="+v)
		}
	}
	return args
}
