package vm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vedantxn/chromebox/internal/firecracker"
)

// DrivePath is where every Firecracker process sees its VM's disk. A snapshot
// records the disk's path, so if each VM's disk lived at a different path,
// concurrent clones of one snapshot would all open the same file. Instead each
// process runs in its own mount namespace in which this one path is bound to
// that VM's own disk file: the path baked into a snapshot is always valid, and
// always means "my disk".
const DrivePath = "/run/chromebox/drive/rootfs.ext4"

// startScript runs as root inside a new mount namespace. It records its own
// pid (which exec preserves, so the pid file names Firecracker itself), binds
// the VM's disk onto DrivePath, then drops to the unprivileged user and becomes
// Firecracker. Firecracker needs no privilege: /dev/kvm is open to all and the
// TAP device is owned by the user.
const startScript = `echo $$ > "$1"
mount --bind "$2" "$3" || exit 1
exec setpriv --reuid="$4" --regid="$5" --clear-groups --bounding-set=-all --inh-caps=-all -- /usr/local/bin/firecracker --api-sock "$6"`

var driveMountReady bool

// ensureDriveMountPoint creates the empty file DrivePath that disks get bound
// onto. /run is tmpfs, so it is gone after a reboot and made again here.
func ensureDriveMountPoint() error {
	if driveMountReady {
		return nil
	}
	if _, err := os.Stat(DrivePath); err != nil {
		out, err := exec.Command("sudo", "sh", "-c",
			"mkdir -p "+driveDir+" && touch "+DrivePath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("create %s: %w: %s", DrivePath, err, out)
		}
	}
	driveMountReady = true
	return nil
}

const driveDir = "/run/chromebox/drive"

// StartProcess launches a Firecracker process for the VM at p and waits until
// its API socket accepts connections. No VM exists yet at that point: the
// process is an empty VMM waiting to be configured or to load a snapshot.
func StartProcess(ctx context.Context, p Paths) (*firecracker.Client, error) {
	if err := ensureDriveMountPoint(); err != nil {
		return nil, err
	}
	// Files left by a dead process would make Firecracker fail to bind.
	for _, f := range []string{p.Socket(), p.VsockSock(), p.PIDFile()} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	// Firecracker's stdout is the guest's serial console, so the log holds
	// both Firecracker's own messages and everything the guest prints.
	log, err := os.OpenFile(p.SerialLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer log.Close() // the child keeps its own copy of the descriptor

	cmd := exec.Command("sudo", "unshare", "--mount", "--propagation", "private", "--",
		"sh", "-c", startScript, "sh",
		p.PIDFile(), p.Rootfs(), DrivePath,
		strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid()), p.Socket())
	cmd.Stdout, cmd.Stderr = log, log
	// A new session detaches the VM from this CLI and from the SSH session
	// that ran it, so the VM keeps running after both exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start firecracker: %w", err)
	}
	_ = cmd.Process.Release()

	if err := waitForSocket(ctx, p.Socket(), 3*time.Second); err != nil {
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

// Kill stops the VM's Firecracker process and removes its TAP device. Killing
// the process alone destroys the VM: when it exits, the kernel closes its KVM
// file descriptors. The TAP is removed even if no process is found, so that a
// failed migration doesn't leave one behind on the target.
func Kill(p Paths) error {
	err := killProcess(p)
	if nn, nerr := NetOf(p); nerr == nil && nn != nil {
		if derr := nn.DeleteTap(); derr != nil && err == nil {
			err = derr
		}
	}
	return err
}

func killProcess(p Paths) error {
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

// UffdSock is where a VM's page-fault handler listens for Firecracker.
func (p Paths) UffdSock() string { return filepath.Join(p.Dir, "uffd.sock") }

// UffdPID holds the handler's pid while the VM's memory is incomplete; the
// handler removes it once every page is in.
func (p Paths) UffdPID() string { return filepath.Join(p.Dir, "uffd.pid") }

// MemoryOrphaned reports a VM whose memory can never complete: its handler
// died before finishing. The guest will hang on its next missing page, so
// such a VM should be killed.
func MemoryOrphaned(p Paths) bool {
	b, err := os.ReadFile(p.UffdPID())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return err == nil && syscall.Kill(pid, 0) != nil
}

// StartUffd starts the page-fault handler for VM p (`chromebox uffd <id>
// args...`) as its own process, detached like Firecracker, so that restarting
// the agent never leaves a VM waiting on a page nobody will serve. It returns
// once the handler listens. The handler exits by itself when every page is in.
func StartUffd(p Paths, args ...string) error {
	_ = os.Remove(p.UffdSock())
	self, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(p.Dir, "uffd.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(self, append([]string{"uffd", filepath.Base(p.Dir)}, args...)...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start uffd handler: %w", err)
	}
	_ = cmd.Process.Release()
	// Wait for the socket file. Not by connecting: the first connection the
	// handler accepts must be Firecracker's.
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if _, err := os.Stat(p.UffdSock()); err == nil {
			return nil
		}
	}
	return fmt.Errorf("uffd handler did not start (see %s/uffd.log)", p.Dir)
}
