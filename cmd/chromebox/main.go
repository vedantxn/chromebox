// Command chromebox manages Firecracker microVMs on the node it runs on.
//
//	chromebox boot <id>     create a VM and boot it
//	chromebox status <id>   ask the VM's Firecracker process for its state
//	chromebox kill <id>     stop the VM's Firecracker process
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/vedantxn/chromebox/internal/firecracker"
	"github.com/vedantxn/chromebox/internal/vm"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: chromebox boot|status|kill <id>")
		os.Exit(2)
	}
	cmd, id := os.Args[1], os.Args[2]
	ctx := context.Background()
	p := vm.PathsFor(id)

	var err error
	switch cmd {
	case "boot":
		err = boot(ctx, p)
	case "status":
		err = status(ctx, p)
	case "kill":
		err = vm.Kill(p)
	default:
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "chromebox:", err)
		os.Exit(1)
	}
}

func boot(ctx context.Context, p vm.Paths) error {
	if err := vm.Create(p); err != nil {
		return err
	}
	c, err := vm.StartProcess(ctx, p)
	if err != nil {
		return err
	}
	if err := vm.Boot(ctx, c, p, vm.Spec{VCPUs: 1, MemMiB: 256}); err != nil {
		return err
	}
	return status(ctx, p)
}

func status(ctx context.Context, p vm.Paths) error {
	info, err := firecracker.New(p.Socket()).DescribeInstance(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s (firecracker %s)\n", p.Dir, info.State, info.VMMVersion)
	return nil
}
