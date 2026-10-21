// Command chromebox manages Firecracker microVMs on the node it runs on.
//
//	chromebox boot <id> [image]       create a VM from a golden image (default "counter") and boot it
//	chromebox status <id>             ask the VM's Firecracker process for its state
//	chromebox pause <id>              stop the VM's vCPUs
//	chromebox resume <id>             start them again
//	chromebox snapshot <id> [nosync]  pause the VM and save vmstate, memory and disk; leaves it paused
//	chromebox restore <id>            bring the VM back from its snapshot, running
//	chromebox kill <id>               stop the VM's Firecracker process
//	chromebox bench-boot <n>          boot n VMs and summarize the timings
//	chromebox bench-snapshot <n>      take n snapshots of a running VM and summarize
//	chromebox bench-restore <n>       restore one snapshot n times and summarize
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

const usage = "usage: chromebox boot|status|pause|resume|snapshot|restore|kill <id> | bench-boot|bench-snapshot|bench-restore <n>"

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	ctx := context.Background()
	cmd, arg := os.Args[1], os.Args[2]
	p := vm.PathsFor(arg)

	var err error
	switch cmd {
	case "boot":
		image := "counter"
		if len(os.Args) > 3 {
			image = os.Args[3]
		}
		var sw *timing.Stopwatch
		if sw, err = boot(ctx, p, image); err == nil {
			sw.Print(os.Stdout)
		}
	case "status":
		err = status(ctx, p)
	case "pause":
		err = firecracker.New(p.Socket()).PauseVM(ctx)
	case "resume":
		err = firecracker.New(p.Socket()).ResumeVM(ctx)
	case "snapshot":
		var sw *timing.Stopwatch
		if sw, err = snapshot(ctx, p, !(len(os.Args) > 3 && os.Args[3] == "nosync")); err == nil {
			sw.Print(os.Stdout)
			printSnapshotFiles(p)
		}
	case "restore":
		err = restoreCmd(ctx, p)
	case "kill":
		err = vm.Kill(p)
	case "bench-boot", "bench-snapshot", "bench-restore":
		var n int
		if n, err = strconv.Atoi(arg); err != nil {
			break
		}
		switch cmd {
		case "bench-boot":
			err = benchBoot(ctx, n)
		case "bench-snapshot":
			err = benchSnapshot(ctx, n)
		default:
			err = benchRestore(ctx, n)
		}
	default:
		err = fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "chromebox:", err)
		os.Exit(1)
	}
}

// boot creates and starts a VM, timing each step from "command started" to
// "the guest's app printed its first output". The laps are saved in the VM's
// directory as timings.json.
func boot(ctx context.Context, p vm.Paths, image string) (*timing.Stopwatch, error) {
	sw := timing.Start()
	if err := vm.Create(p, image); err != nil {
		return nil, err
	}
	sw.Lap("disk cloned")
	c, err := vm.StartProcess(ctx, p)
	if err != nil {
		return nil, err
	}
	sw.Lap("firecracker API ready")
	if err := vm.Configure(ctx, c, p, vm.Spec{VCPUs: 1, MemMiB: 256}); err != nil {
		return nil, err
	}
	sw.Lap("configured")
	if err := c.StartInstance(ctx); err != nil {
		return nil, err
	}
	sw.Lap("InstanceStart returned")
	if _, err := vm.WaitForCount(ctx, p, 0, 30*time.Second); err != nil {
		return nil, err
	}
	sw.Lap("first count (ready)")
	return sw, sw.Save(p.Timings())
}

func snapshot(ctx context.Context, p vm.Paths, sync bool) (*timing.Stopwatch, error) {
	sw := timing.Start()
	err := vm.Snapshot(ctx, firecracker.New(p.Socket()), p, vm.SnapshotOptions{Sync: sync}, sw)
	return sw, err
}

// restore brings a VM back from its snapshot and waits for the guest to count
// again. It returns the last count printed before the restore and the first
// count printed after it: if the VM truly resumed, the second is just past the
// first; a reboot would restart at 1.
func restore(ctx context.Context, p vm.Paths) (sw *timing.Stopwatch, before, after int, err error) {
	before, err = vm.LastCount(p)
	if err != nil {
		return nil, 0, 0, err
	}
	from := vm.LogSize(p) // ignore every count line printed before this moment
	sw = timing.Start()
	if _, err = vm.Restore(ctx, p, sw); err != nil {
		return nil, 0, 0, err
	}
	if after, err = vm.WaitForCount(ctx, p, from, 30*time.Second); err != nil {
		return nil, 0, 0, err
	}
	sw.Lap("first new count (ready)")
	return sw, before, after, nil
}

func restoreCmd(ctx context.Context, p vm.Paths) error {
	sw, before, after, err := restore(ctx, p)
	if err != nil {
		return err
	}
	sw.Print(os.Stdout)
	fmt.Printf("\nlast count before: %d   first count after: %d\n", before, after)
	return nil
}

func status(ctx context.Context, p vm.Paths) error {
	info, err := firecracker.New(p.Socket()).DescribeInstance(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s (firecracker %s)\n", p.Dir, info.State, info.VMMVersion)
	return nil
}

// printSnapshotFiles shows each snapshot file's apparent size and how much disk
// it really occupies (they differ for sparse files).
func printSnapshotFiles(p vm.Paths) {
	fmt.Println()
	for _, f := range []string{p.Vmstate(), p.Mem(), p.SnapRootfs()} {
		var st syscall.Stat_t
		if err := syscall.Stat(f, &st); err != nil {
			continue
		}
		fmt.Printf("%-44s %9.2f MiB apparent %9.2f MiB on disk\n", f, mib(st.Size), mib(st.Blocks*512))
	}
}

func mib(b int64) float64 { return float64(b) / (1 << 20) }

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// samples collects a stopwatch per run and summarizes each lap and the total.
type samples struct {
	names   []string
	perStep map[string][]time.Duration
	totals  []time.Duration
}

func newSamples() *samples { return &samples{perStep: map[string][]time.Duration{}} }

func (s *samples) add(sw *timing.Stopwatch) {
	steps, total := sw.Steps()
	s.names = sw.Names()
	for name, d := range steps {
		s.perStep[name] = append(s.perStep[name], d)
	}
	s.totals = append(s.totals, total)
}

func (s *samples) print(totalLabel string) {
	fmt.Printf("%-26s %9s %9s %9s   (n=%d)\n", "step", "p50 ms", "p95 ms", "max ms", len(s.totals))
	for _, name := range s.names {
		x := timing.Summarize(s.perStep[name])
		fmt.Printf("%-26s %9.1f %9.1f %9.1f\n", name, ms(x.P50), ms(x.P95), ms(x.Max))
	}
	x := timing.Summarize(s.totals)
	fmt.Printf("%-26s %9.1f %9.1f %9.1f\n", totalLabel, ms(x.P50), ms(x.P95), ms(x.Max))
}

// benchBoot boots n counter VMs one at a time and kills each once it's ready.
func benchBoot(ctx context.Context, n int) error {
	s := newSamples()
	for i := 1; i <= n; i++ {
		p := vm.PathsFor(fmt.Sprintf("bench-%d", i))
		_ = os.RemoveAll(p.Dir)
		sw, err := boot(ctx, p, "counter")
		if err != nil {
			return fmt.Errorf("run %d: %w", i, err)
		}
		if err := vm.Kill(p); err != nil {
			return err
		}
		_ = os.Remove(p.Rootfs())
		s.add(sw)
	}
	s.print("TOTAL (ask -> ready)")
	return nil
}

// benchSnapshot boots one VM, then pauses, snapshots and resumes it n times
// with fsync on and n times with it off.
func benchSnapshot(ctx context.Context, n int) error {
	p := vm.PathsFor("sbench")
	_ = os.RemoveAll(p.Dir)
	if _, err := boot(ctx, p, "counter"); err != nil {
		return err
	}
	defer vm.Kill(p)
	time.Sleep(time.Second) // let the guest run a little before the first snapshot

	for _, sync := range []bool{true, false} {
		s := newSamples()
		for i := 0; i < n; i++ {
			sw, err := snapshot(ctx, p, sync)
			if err != nil {
				return fmt.Errorf("snapshot %d: %w", i+1, err)
			}
			if err := firecracker.New(p.Socket()).ResumeVM(ctx); err != nil {
				return err
			}
			s.add(sw)
			time.Sleep(200 * time.Millisecond) // let the guest dirty some memory between snapshots
		}
		fmt.Printf("\nsnapshot with sync_snapshot_files=%v\n", sync)
		s.print("TOTAL (pause -> done)")
	}
	printSnapshotFiles(p)
	return nil
}

// benchRestore boots a VM, snapshots it, kills it, then restores that one
// snapshot n times, killing the VM after each.
func benchRestore(ctx context.Context, n int) error {
	p := vm.PathsFor("rbench")
	_ = os.RemoveAll(p.Dir)
	if _, err := boot(ctx, p, "counter"); err != nil {
		return err
	}
	time.Sleep(time.Second)
	if _, err := snapshot(ctx, p, true); err != nil {
		return err
	}
	if err := vm.Kill(p); err != nil {
		return err
	}
	defer vm.Kill(p)

	s := newSamples()
	atSnapshot, err := vm.LastCount(p)
	if err != nil {
		return err
	}
	var resumedAt []int
	for i := 1; i <= n; i++ {
		sw, _, after, err := restore(ctx, p)
		if err != nil {
			return fmt.Errorf("restore %d: %w", i, err)
		}
		resumedAt = append(resumedAt, after)
		s.add(sw)
		if err := vm.Kill(p); err != nil {
			return err
		}
	}
	s.print("TOTAL (ask -> ready)")
	fmt.Printf("\ncounter at snapshot: %d; first count after each restore: %v\n", atSnapshot, resumedAt)
	return nil
}
