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

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

// Result describes one finished migration.
type Result struct {
	Laps     *timing.Stopwatch // steps on this node, from the start of migrate
	Downtime time.Duration     // VM paused here -> first new count on the target
	Before   int               // last count printed here before the pause
	After    int               // first count printed on the target after the restore
	Remote   timing.Record     // the target's own breakdown of its restore
}

// Report is what `chromebox accept` prints on the target for the source to read.
type Report struct {
	After int           `json:"after"`
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
	r := newRemote(host)
	c := firecracker.New(p.Socket())
	targetStarted := false

	defer func() {
		if err == nil {
			return
		}
		if targetStarted {
			_, _ = r.run(context.Background(), "chromebox", "kill", filepath.Base(p.Dir))
		}
		_, _ = r.run(context.Background(), "rm", "-rf", p.Dir)
		_ = c.ResumeVM(context.Background()) // errors if it was never paused; fine
		err = fmt.Errorf("migration rolled back, source resumed: %w", err)
	}()

	// Everything before the pause is preparation and is not downtime.
	if _, err := r.run(ctx, "true"); err != nil { // opens the shared ssh connection
		return nil, fmt.Errorf("cannot reach %s: %w", host, err)
	}
	sw.Lap("ssh connected")
	if _, err := r.run(ctx, "mkdir", "-p", p.SnapDir()); err != nil {
		return nil, err
	}
	sw.Lap("target prepared")
	if err := r.copyDisk(ctx, p, "-aS"); err != nil {
		return nil, err
	}
	sw.Lap("disk pre-copied")

	downStart := time.Now()
	if err := vm.Snapshot(ctx, c, p, vm.SnapshotOptions{Sync: false}, sw); err != nil {
		return nil, err
	}
	before, err := vm.LastCount(p)
	if err != nil {
		return nil, err
	}

	// Memory and the disk delta are independent, so send them at the same time
	// over the shared connection; the slower one sets the pace.
	if err := r.copyState(ctx, p); err != nil {
		return nil, err
	}
	sw.Lap("memory + disk delta copied")

	targetStarted = true
	out, err := r.run(ctx, "chromebox", "accept", filepath.Base(p.Dir))
	if err != nil {
		return nil, err
	}
	var rep Report
	if err := json.Unmarshal(out, &rep); err != nil {
		return nil, fmt.Errorf("bad report from target: %w: %s", err, out)
	}
	down := time.Since(downStart)
	sw.Lap("restored on target")

	// The VM resumed from the snapshot if its next count follows the last one
	// printed here. A reboot would print 1; a stale or wrong snapshot would
	// land somewhere else entirely.
	if rep.After <= before || rep.After > before+5 {
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

	return &Result{Laps: sw, Downtime: down, Before: before, After: rep.After, Remote: rep.Laps}, nil
}

// remote runs commands on the target over one shared ssh connection, so that
// setting up the connection (a TCP handshake plus key exchange) is paid once,
// before the pause, and not inside the downtime.
type remote struct {
	host string
	opts []string
}

func newRemote(host string) *remote {
	home, _ := os.UserHomeDir()
	return &remote{host: host, opts: []string{
		"-i", filepath.Join(home, ".ssh", "chromebox_node"),
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=/tmp/chromebox-ssh-%C",
		"-o", "ControlPersist=60",
	}}
}

// run executes a command on the target and returns its stdout.
func (r *remote) run(ctx context.Context, argv ...string) ([]byte, error) {
	args := append(append([]string{}, r.opts...), r.host)
	args = append(args, argv...)
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh %s %v: %w: %s", r.host, argv, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

// sshCmd is the ssh command line rsync should use, sharing our connection.
func (r *remote) sshCmd() string {
	return "ssh " + strings.Join(r.opts, " ")
}

// copyMemory streams the memory and device-state files of the snapshot to the
// same path on the target: tar here, through the shared ssh connection, into
// tar there. The disk image is excluded; copyDisk handles it.
//
// --sparse writes the runs of zeros as holes, so they cost nothing to send or
// store (about 60% of a fresh guest's memory pages are all zeros). No
// compression: it costs more CPU time than the network takes to move bytes.
func (r *remote) copyMemory(ctx context.Context, p vm.Paths) error {
	send := exec.CommandContext(ctx, "tar", "-C", p.SnapDir(), "--sparse", "--exclude=rootfs.ext4", "-cf", "-", ".")
	args := append(append([]string{}, r.opts...), r.host, "tar", "-C", p.SnapDir(), "-xf", "-")
	recv := exec.CommandContext(ctx, "ssh", args...)

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
func (r *remote) copyState(ctx context.Context, p vm.Paths) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errc := make(chan error, 2)
	go func() { errc <- r.copyMemory(ctx, p) }()
	go func() { errc <- r.copyDisk(ctx, p, "-aI", "--inplace") }()
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
func (r *remote) copyDisk(ctx context.Context, p vm.Paths, flags ...string) error {
	args := append(flags, "-e", r.sshCmd(), p.Rootfs(), r.host+":"+p.SnapRootfs())
	if out, err := exec.CommandContext(ctx, "rsync", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("rsync disk: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// Discard destroys a VM on the target. Benchmarks use it to clean up.
func Discard(ctx context.Context, p vm.Paths, host string) error {
	r := newRemote(host)
	_, _ = r.run(ctx, "chromebox", "kill", filepath.Base(p.Dir))
	_, err := r.run(ctx, "rm", "-rf", p.Dir)
	return err
}
