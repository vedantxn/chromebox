package vm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vedantxn/chromebox/internal/firecracker"
)

// StartProcess launches a Firecracker process for the VM at p and waits until
// its API socket accepts connections. No VM exists yet at that point: the
// process is an empty VMM waiting to be configured.
func StartProcess(ctx context.Context, p Paths) (*firecracker.Client, error) {
	// A socket file left by a dead process would make Firecracker fail to bind.
	if err := os.Remove(p.Socket()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Firecracker's stdout is the guest's serial console, so the log holds
	// both Firecracker's own messages and everything the guest prints.
	log, err := os.OpenFile(p.SerialLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer log.Close() // the child keeps its own copy of the descriptor

	cmd := exec.Command("firecracker", "--api-sock", p.Socket())
	cmd.Stdout, cmd.Stderr = log, log
	// A new session detaches the VM from this CLI and from the SSH session
	// that ran it, so the VM keeps running after both exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start firecracker: %w", err)
	}
	pid := cmd.Process.Pid
	if err := os.WriteFile(p.PIDFile(), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return nil, err
	}
	_ = cmd.Process.Release()

	if err := waitForSocket(ctx, p.Socket(), 2*time.Second); err != nil {
		return nil, fmt.Errorf("%w (see %s)", err, p.SerialLog())
	}
	return firecracker.New(p.Socket()), nil
}

// waitForSocket polls until something accepts connections on path.
func waitForSocket(ctx context.Context, path string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		conn, err := net.Dial("unix", path)
		if err == nil {
			return conn.Close()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("firecracker API socket %s not ready: %w", path, err)
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// Kill stops the VM's Firecracker process. That alone destroys the VM: when
// the process exits, the kernel closes its KVM file descriptors.
func Kill(p Paths) error {
	b, err := os.ReadFile(p.PIDFile())
	if err != nil {
		return fmt.Errorf("no running VM in %s: %w", p.Dir, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("bad pid file %s: %w", p.PIDFile(), err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	_ = os.Remove(p.PIDFile())
	_ = os.Remove(p.Socket())
	return nil
}
