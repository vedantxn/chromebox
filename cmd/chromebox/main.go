// Command chromebox manages Firecracker microVMs on the node it runs on.
//
//	chromebox boot <id> [image] [net=N]  create a VM from a golden image (default "counter") and boot it;
//	                                  net=N gives it network index N (see internal/netsetup)
//	chromebox status <id>             ask the VM's Firecracker process for its state
//	chromebox pause <id>              stop the VM's vCPUs
//	chromebox resume <id>             start them again
//	chromebox snapshot <id> [nosync]  pause the VM and save vmstate, memory and disk; leaves it paused
//	chromebox restore <id>            bring the VM back from its snapshot, running
//	chromebox kill <id>               stop the VM's Firecracker process
//	chromebox gateway [listen]        run the CDP proxy (on the gateway host; default :9222)
//	chromebox migrate <id> <host>     move a running VM to another node (host: ssh target)
//	chromebox accept <id>             target side of migrate: restore a received snapshot, print a JSON report
//	chromebox bench-boot <n> [image]  boot n VMs and summarize the timings
//	chromebox bench-snapshot <n>      take n snapshots of a running VM and summarize
//	chromebox bench-restore <n>       restore one snapshot n times and summarize
//	chromebox bench-migrate <n> <host> [gap-seconds]  migrate n freshly booted VMs and summarize
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/gateway"
	"github.com/vedantxn/chromebox/internal/migrate"
	"github.com/vedantxn/chromebox/internal/node"
	"github.com/vedantxn/chromebox/internal/remote"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

const usage = "usage: chromebox boot|status|pause|resume|snapshot|restore|kill|accept <id> | migrate <id> <host> | bench-boot|bench-snapshot|bench-restore <n> | bench-migrate <n> <host>"

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
		image, netIndex := "counter", 0
		for _, a := range os.Args[3:] {
			if v, ok := strings.CutPrefix(a, "net="); ok {
				if netIndex, err = strconv.Atoi(v); err != nil {
					break
				}
			} else {
				image = a
			}
		}
		if err != nil {
			break
		}
		var sw *timing.Stopwatch
		if sw, err = boot(ctx, p, image, netIndex); err == nil {
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
		err = killCmd(ctx, p)
	case "gateway":
		err = runGateway(arg)
	case "accept":
		err = accept(ctx, p)
	case "migrate":
		if len(os.Args) < 4 {
			err = fmt.Errorf("usage: chromebox migrate <id> <host>")
			break
		}
		err = migrateCmd(ctx, p, os.Args[3])
	case "bench-migrate":
		if len(os.Args) < 4 {
			err = fmt.Errorf("usage: chromebox bench-migrate <n> <host>")
			break
		}
		var n int
		gap := 5
		if len(os.Args) > 4 {
			if gap, err = strconv.Atoi(os.Args[4]); err != nil {
				break
			}
		}
		if n, err = strconv.Atoi(arg); err == nil {
			err = benchMigrate(ctx, n, os.Args[3], time.Duration(gap)*time.Second)
		}
	case "bench-boot", "bench-snapshot", "bench-restore":
		var n int
		if n, err = strconv.Atoi(arg); err != nil {
			break
		}
		switch cmd {
		case "bench-boot":
			image := "counter"
			if len(os.Args) > 3 {
				image = os.Args[3]
			}
			err = benchBoot(ctx, n, image)
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
func boot(ctx context.Context, p vm.Paths, image string, netIndex int) (*timing.Stopwatch, error) {
	img, err := vm.LookupImage(image)
	if err != nil {
		return nil, err
	}
	sw := timing.Start()
	if err := vm.Create(p, image); err != nil {
		return nil, err
	}
	sw.Lap("disk cloned")
	spec := vm.Spec{VCPUs: img.VCPUs, MemMiB: img.MemMiB}
	if netIndex > 0 {
		if spec.Net, err = vm.AttachNet(p, netIndex); err != nil {
			return nil, err
		}
		sw.Lap("tap ready")
	}
	c, err := vm.StartProcess(ctx, p)
	if err != nil {
		return nil, err
	}
	sw.Lap("firecracker API ready")
	if err := vm.Configure(ctx, c, p, spec); err != nil {
		return nil, err
	}
	sw.Lap("configured")
	if err := c.StartInstance(ctx); err != nil {
		return nil, err
	}
	sw.Lap("InstanceStart returned")
	if _, err := vm.WaitForMatch(ctx, p, img.Ready, 0, 60*time.Second); err != nil {
		return nil, err
	}
	sw.Lap("app ready")
	if spec.Net != nil && img.CDPPort != 0 {
		addr := fmt.Sprintf("%s:%d", spec.Net.GuestIP, img.CDPPort)
		if err := vm.WaitTCP(ctx, addr, 10*time.Second); err != nil {
			return nil, err
		}
		sw.Lap("CDP reachable from host")
	}
	// Tell the gateway where this VM lives, so agents can reach it.
	if cfg, err := node.Load(); err != nil {
		return nil, err
	} else if cfg != nil && cfg.Gateway != "" && spec.Net != nil {
		if err := gateway.SetRoute(ctx, remote.New(cfg.Gateway), spec.Net.Block(), cfg.Tunnel()); err != nil {
			return nil, fmt.Errorf("gateway route: %w", err)
		}
		sw.Lap("gateway route set")
	}
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
	if _, img, ierr := vm.ImageOf(p); ierr == nil && img.Counter {
		if before, err = vm.LastCount(p); err != nil {
			return nil, 0, 0, err
		}
	}
	from := vm.LogSize(p) // ignore every count line printed before this moment
	sw = timing.Start()
	if _, err = vm.Restore(ctx, p, sw); err != nil {
		return nil, 0, 0, err
	}
	if after, err = vm.WaitRestored(ctx, p, from, 30*time.Second); err != nil {
		return nil, 0, 0, err
	}
	sw.Lap("running again")
	return sw, before, after, nil
}

func restoreCmd(ctx context.Context, p vm.Paths) error {
	sw, before, after, err := restore(ctx, p)
	if err != nil {
		return err
	}
	sw.Print(os.Stdout)
	if after > 0 { // only the counter guest has counts to compare
		fmt.Printf("\nlast count before: %d   first count after: %d\n", before, after)
	}
	return nil
}

// accept is the target half of a migration. The source has copied a snapshot
// into this node's p.SnapDir(); restore it and report the first count the guest
// prints, as JSON on stdout (nothing else may be printed there).
func accept(ctx context.Context, p vm.Paths) error {
	from := vm.LogSize(p)
	sw := timing.Start()
	if _, err := vm.Restore(ctx, p, sw); err != nil {
		return err
	}
	after, err := vm.WaitRestored(ctx, p, from, 30*time.Second)
	if err != nil {
		return err
	}
	sw.Lap("running again")
	rep := migrate.Report{After: after, Laps: sw.Record()}
	if cfg, err := node.Load(); err == nil && cfg != nil {
		rep.Node = cfg.Name
	}
	return json.NewEncoder(os.Stdout).Encode(rep)
}

func migrateCmd(ctx context.Context, p vm.Paths, host string) error {
	res, err := migrate.Run(ctx, p, host)
	if err != nil {
		return err
	}
	fmt.Println("on this node:")
	res.Laps.Print(os.Stdout)
	fmt.Println("\non the target (inside 'restored on target'):")
	for _, l := range res.Remote.Laps {
		fmt.Printf("%-26s %10.1f\n", l.Name, l.StepMs)
	}
	fmt.Printf("\nlast count on source: %d   first count on target: %d\n", res.Before, res.After)
	fmt.Printf("DOWNTIME (paused -> running on target): %.1f ms\n", ms(res.Downtime))
	if res.Reachable > 0 {
		fmt.Printf("UNREACHABLE (paused -> gateway route flipped): %.1f ms\n", ms(res.Reachable))
	}
	return nil
}

// killCmd stops a VM for good. When the node has a gateway it also removes the
// VM's route there. (vm.Kill alone does not: migration kills the source copy
// after the route has already moved to the target, and must leave it alone.)
func killCmd(ctx context.Context, p vm.Paths) error {
	nn, _ := vm.NetOf(p)
	err := vm.Kill(p)
	if cfg, _ := node.Load(); cfg != nil && cfg.Gateway != "" && nn != nil {
		_ = gateway.DelRoute(ctx, remote.New(cfg.Gateway), nn.Block())
	}
	return err
}

func runGateway(listen string) error {
	if !strings.Contains(listen, ":") {
		listen = ":9222"
	}
	srv := &http.Server{Addr: listen, Handler: gateway.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("CDP proxy listening on %s", listen)
	return srv.ListenAndServe()
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

// addRecord adds a run reported as data (e.g. by another node) instead of
// measured by a local stopwatch.
func (s *samples) addRecord(r timing.Record) {
	s.names = s.names[:0]
	for _, l := range r.Laps {
		s.names = append(s.names, l.Name)
		s.perStep[l.Name] = append(s.perStep[l.Name], time.Duration(l.StepMs*float64(time.Millisecond)))
	}
	s.totals = append(s.totals, time.Duration(r.TotalMs*float64(time.Millisecond)))
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

// benchBoot boots n VMs of an image one at a time and kills each once it's ready.
func benchBoot(ctx context.Context, n int, image string) error {
	s := newSamples()
	for i := 1; i <= n; i++ {
		p := vm.PathsFor(fmt.Sprintf("bench-%d", i))
		_ = os.RemoveAll(p.Dir)
		sw, err := boot(ctx, p, image, 0)
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
	if _, err := boot(ctx, p, "counter", 0); err != nil {
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
	if _, err := boot(ctx, p, "counter", 0); err != nil {
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

// benchMigrate boots n counter VMs on this node, migrates each to host, and
// summarizes the steps on both nodes and the downtime. gap is the pause between
// runs, so one migration's disk write-back doesn't slow the next.
func benchMigrate(ctx context.Context, n int, host string, gap time.Duration) error {
	local, remote := newSamples(), newSamples()
	var downtimes []time.Duration
	for i := 1; i <= n; i++ {
		p := vm.PathsFor(fmt.Sprintf("mig-%d", i))
		_ = os.RemoveAll(p.Dir)
		if _, err := boot(ctx, p, "counter", 0); err != nil {
			return fmt.Errorf("run %d: boot: %w", i, err)
		}
		time.Sleep(time.Second) // let the guest run and dirty some memory first
		res, err := migrate.Run(ctx, p, host)
		if err != nil {
			return fmt.Errorf("run %d: %w", i, err)
		}
		local.add(res.Laps)
		remote.addRecord(res.Remote)
		downtimes = append(downtimes, res.Downtime)
		fmt.Printf("run %2d: downtime %7.1f ms   (count %d -> %d)\n", i, ms(res.Downtime), res.Before, res.After)
		if err := migrate.Discard(ctx, p, host); err != nil {
			return err
		}
		time.Sleep(gap) // let the target's disk write-back settle between runs
	}
	fmt.Printf("\non the source node:\n")
	local.print("TOTAL (whole migrate)")
	fmt.Printf("\non the target node (inside 'restored on target'):\n")
	remote.print("TOTAL (restore on target)")
	d := timing.Summarize(downtimes)
	fmt.Printf("\nDOWNTIME (paused -> counting on target), n=%d:  p50 %.1f ms   p95 %.1f ms   max %.1f ms\n", len(downtimes), ms(d.P50), ms(d.P95), ms(d.Max))
	return nil
}
