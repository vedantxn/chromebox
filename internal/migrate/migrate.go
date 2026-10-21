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
	"time"

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
}

// Report is what `chromebox accept` prints on the target for the source to read.
type Report struct {
	After int           `json:"after"`
	Node  string        `json:"node"` // the target's name, which names its tunnel on the gateway
	Laps  timing.Record `json:"laps"`
}

// Run migrates VM p to host (an ssh target such as ec2-user@172.31.2.181).
//
// The disk is copied twice. The first pass runs while the VM is still running
// and costs no downtime; it moves the bulk (reading a cold 400 MB disk image
// from EBS is the slowest thing in the whole migration). The second pass runs
// after the pause and sends only the blocks that changed in between. The same
// idea, "copy while it runs, then send what changed", is how live migration
// moves memory.
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
		_, _ = r.Run(context.Background(), "rm", "-rf", p.Dir)
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
	if _, err := r.Run(ctx, "mkdir", "-p", p.SnapDir()); err != nil {
		return nil, err
	}
	sw.Lap("target prepared")
	if err := copyDisk(ctx, r, p, "-aS"); err != nil {
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
	if err := copyState(ctx, r, p); err != nil {
		return nil, err
	}
	sw.Lap("memory + disk delta copied")

	targetStarted = true
	out, err := r.Run(ctx, "chromebox", "accept", filepath.Base(p.Dir))
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
	if gw != nil {
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
	sw.Lap("source destroyed")

	return &Result{Laps: sw, Downtime: down, Reachable: reachable, Before: before, After: rep.After, Remote: rep.Laps}, nil
}

// copyMemory streams the memory and device-state files of the snapshot to the
// same path on the target: tar here, through the shared ssh connection, into
// tar there. The disk image is excluded; copyDisk handles it.
//
// --sparse writes the runs of zeros as holes, so they cost nothing to send or
// store (about 60% of a fresh guest's memory pages are all zeros). No
// compression: it costs more CPU time than the network takes to move bytes.
func copyMemory(ctx context.Context, r *remote.Remote, p vm.Paths) error {
	send := exec.CommandContext(ctx, "tar", "-C", p.SnapDir(), "--sparse", "--exclude=rootfs.ext4", "-cf", "-", ".")
	recv := r.Command(ctx, "tar", "-C", p.SnapDir(), "-xf", "-")

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

// copyState sends the memory files and the disk delta concurrently and returns
// the first error, if any. The disk has been paused since the snapshot started,
// so p.Rootfs() is exactly what the target must end up with. rsync's -I forces
// a block-level comparison even when size and time look unchanged, and
// --inplace rewrites only the blocks that differ in the file the first pass
// already put there.
func copyState(ctx context.Context, r *remote.Remote, p vm.Paths) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	go func() { errc <- copyMemory(ctx, r, p) }()
	go func() { errc <- copyDisk(ctx, r, p, "-aI", "--inplace") }()
	var first error
	for i := 0; i < 2; i++ {
		if err := <-errc; err != nil && first == nil {
			first = err
			cancel() // stop the other transfer; its error is not interesting
		}
	}
	return first
}

// copyDisk brings the target's snapshot disk image up to date with this VM's
// disk, using rsync, which sends only the blocks that differ from what the
// target already has. flags are rsync options.
func copyDisk(ctx context.Context, r *remote.Remote, p vm.Paths, flags ...string) error {
	args := append(flags, "-e", r.SSHCmd(), p.Rootfs(), r.Host+":"+p.SnapRootfs())
	if out, err := exec.CommandContext(ctx, "rsync", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("rsync disk: %w: %s", err, bytes.TrimSpace(out))
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
