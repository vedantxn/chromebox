// Command chromebox manages Firecracker microVMs on the node it runs on.
//
//	chromebox boot <id> [image]   create a VM from a golden image (default "counter") and boot it
//	chromebox status <id>         ask the VM's Firecracker process for its state
//	chromebox kill <id>           stop the VM's Firecracker process
//	chromebox bench-boot <n>      boot n VMs one after another and summarize the timings
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/timing"
	"github.com/vedantxn/chromebox/internal/vm"
)

const usage = "usage: chromebox boot <id> [image] | status <id> | kill <id> | bench-boot <n>"

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	ctx := context.Background()
	cmd, arg := os.Args[1], os.Args[2]

	var err error
	switch cmd {
	case "boot":
		image := "counter"
		if len(os.Args) > 3 {
			image = os.Args[3]
		}
		var sw *timing.Stopwatch
		if sw, err = boot(ctx, vm.PathsFor(arg), image); err == nil {
			sw.Print(os.Stdout)
		}
	case "status":
		err = status(ctx, vm.PathsFor(arg))
	case "kill":
		err = vm.Kill(vm.PathsFor(arg))
	case "bench-boot":
		var n int
		if n, err = strconv.Atoi(arg); err == nil {
			err = benchBoot(ctx, n)
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
	if err := vm.WaitForConsole(ctx, p, "count 1", 30*time.Second); err != nil {
		return nil, err
	}
	sw.Lap("first count (ready)")
	return sw, sw.Save(p.Timings())
}

// benchBoot boots n counter VMs one at a time, kills each once it's ready,
// and prints p50/p95/max for every step and for the total.
func benchBoot(ctx context.Context, n int) error {
	var names []string
	perStep := map[string][]time.Duration{}
	var totals []time.Duration

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
		_ = os.Remove(p.Rootfs()) // keep timings.json and serial.log, drop the disk

		steps, total := sw.Steps()
		names = sw.Names()
		for name, d := range steps {
			perStep[name] = append(perStep[name], d)
		}
		totals = append(totals, total)
		fmt.Printf("run %2d: %7.1f ms\n", i, ms(total))
	}

	fmt.Printf("\n%-26s %9s %9s %9s   (n=%d)\n", "step", "p50 ms", "p95 ms", "max ms", n)
	for _, name := range names {
		s := timing.Summarize(perStep[name])
		fmt.Printf("%-26s %9.1f %9.1f %9.1f\n", name, ms(s.P50), ms(s.P95), ms(s.Max))
	}
	s := timing.Summarize(totals)
	fmt.Printf("%-26s %9.1f %9.1f %9.1f\n", "TOTAL (ask -> ready)", ms(s.P50), ms(s.P95), ms(s.Max))
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

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
