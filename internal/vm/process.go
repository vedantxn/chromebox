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
// process sees, inside its own jail, this one path bound to that VM's own disk
// file: the path baked into a snapshot is always valid, and always means "my
// disk".
const DrivePath = "/run/chromebox/drive/rootfs.ext4"

// JailBase is where the jailer makes each VM's chroot: <JailBase>/firecracker/<id>/root.
const JailBase = "/srv/jailer"

// jailRoot is the chroot of VM p.
func jailRoot(p Paths) string {
	return filepath.Join(JailBase, "firecracker", filepath.Base(p.Dir), "root")
}

// cgroupDir is VM p's cgroup (v2), made by the jailer under chromebox/.
func cgroupDir(p Paths) string {
	return filepath.Join("/sys/fs/cgroup/chromebox", filepath.Base(p.Dir))
}

// startScript runs as root inside a new, private mount namespace, so nothing
// it mounts is seen by the host. It builds the VM's jail: the VM's disk bound
// onto DrivePath, and each directory the VM needs bound at the same absolute
// path it has on the host (read-only where marked with a leading "ro:"), so
// every path in the code and in existing snapshots stays valid inside. Then it
// becomes Firecracker's jailer, which chroots there, makes the device nodes
// (kvm, tun, urandom, userfaultfd), puts the VM in its own PID namespace and
// cgroup, drops to the unprivileged user and execs Firecracker. The jailer's
// own mount namespace inherits these binds.
//
//	$1 jail root  $2 disk  $3 n  $4..$(3+n) directories  then the jailer's arguments
const startScript = `J="$1"; disk="$2"; n="$3"; shift 3
mkdir -p "$J/run/chromebox/drive" && touch "$J/run/chromebox/drive/rootfs.ext4" || exit 1
mount --bind "$disk" "$J/run/chromebox/drive/rootfs.ext4" || exit 1
while [ "$n" -gt 0 ]; do
  d="$1"; shift; n=$((n-1)); ro=
  case "$d" in ro:*) ro=1; d=${d#ro:};; esac
  mkdir -p "$J$d" && mount --bind "$d" "$J$d" || exit 1
  [ -z "$ro" ] || mount -o remount,bind,ro "$J$d" || exit 1
done
exec jailer "$@"`

// limits are the cgroup limits each VM gets. memory.max covers the guest plus
// Firecracker itself; pids.max stops a runaway VMM. CPU: every VM's cgroup has
// the same cpu.weight, so under contention VMs get equal shares whatever
// number of busy threads each has, and cpu.max caps a VM at one CPU, so a busy
// browser cannot take a 2-CPU node. Equal weight alone left a neighbour's p99
// at 3x its idle value; the cap kept it near idle (docs/learn/prod.md, J2).
// The period is 20 ms, not the default 100 ms: a capped VM at its quota
// sleeps until the next period, Firecracker's own threads included, and a
// short period bounds that wait (a busy capped VM still migrates with a
// 217-242 ms pause). CHROMEBOX_VM_CPU_MAX overrides it ("max": no cap).
func limits(p Paths) []string {
	cpu := "20000 20000"
	if v := os.Getenv("CHROMEBOX_VM_CPU_MAX"); v != "" {
		cpu = v
	}
	out := []string{"pids.max=64", "cpu.max=" + cpu}
	if _, img, err := ImageOf(p); err == nil {
		out = append(out, fmt.Sprintf("memory.max=%d", int64(img.MemMiB+256)<<20))
	}
	return out
}

// StartProcess launches a jailed Firecracker for the VM at p and waits until
// its API socket accepts connections. No VM exists yet at that point: the
// process is an empty VMM waiting to be configured or to load a snapshot.
// readOnly lists further host directories the VM must see (read-only), such
// as the base or fork image it is restored from; its own directories (p.Dir,
// p.MemDir()) and the kernels are always there.
func StartProcess(ctx context.Context, p Paths, readOnly ...string) (*firecracker.Client, error) {
	// Files left by a dead process would make Firecracker fail to bind.
	for _, f := range []string{p.Socket(), p.VsockSock(), p.PIDFile()} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	removeJail(p) // a jail left by a previous process of this VM
	if err := os.MkdirAll(p.MemDir(), 0o755); err != nil {
		return nil, err
	}

	// Firecracker's stdout is the guest's serial console, so the log holds
	// both Firecracker's own messages and everything the guest prints.
	log, err := os.OpenFile(p.SerialLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	defer log.Close() // the child keeps its own copy of the descriptor

	id := filepath.Base(p.Dir)
	args := []string{"--id", id, "--exec-file", "/usr/local/bin/firecracker",
		"--uid", strconv.Itoa(os.Getuid()), "--gid", strconv.Itoa(os.Getgid()),
		"--chroot-base-dir", JailBase, "--new-pid-ns", "--cgroup-version", "2", "--parent-cgroup", "chromebox"}
	for _, l := range limits(p) {
		args = append(args, "--cgroup", l)
	}
	args = append(args, "--", "--api-sock", p.Socket())
	dirs := []string{p.Dir, p.MemDir(), "ro:" + filepath.Dir(KernelPath)}
	for _, d := range readOnly {
		dirs = append(dirs, "ro:"+d)
	}
	argv := append([]string{"unshare", "--mount", "--propagation", "private", "--",
		"sh", "-c", startScript, "sh", jailRoot(p), p.Rootfs(), strconv.Itoa(len(dirs))}, dirs...)
	cmd := exec.Command("sudo", append(argv, args...)...)
	cmd.Stdout, cmd.Stderr = log, log
	// A new session detaches the VM from this process and from any SSH
	// session that ran it, so the VM keeps running after both exit.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start firecracker: %w", err)
	}
	_ = cmd.Process.Release()

	if err := waitForSocket(ctx, p.Socket(), 3*time.Second); err != nil {
		return nil, fmt.Errorf("%w (see %s)", err, p.SerialLog())
	}
	// In its own PID namespace Firecracker is PID 1; the jailer writes its
	// host PID into the jail (whose root belongs to our user). Keep it where
	// Kill and the agent look for it.
	var b []byte
	for i := 0; i < 50; i++ {
		if b, err = os.ReadFile(filepath.Join(jailRoot(p), "firecracker.pid")); err == nil && len(b) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("firecracker pid: %w", err)
	}
	if err := os.WriteFile(p.PIDFile(), b, 0o644); err != nil {
		return nil, err
	}
	return firecracker.New(p.Socket()), nil
}

// removeJail deletes VM p's jail directory and cgroup. The jail's bind mounts
// lived only in the VM's private mount namespace, so with the process gone
// the directory holds plain files (a copy of the binary, empty mount points);
// --one-file-system makes sure nothing outside it could ever be reached.
func removeJail(p Paths) {
	if _, err := os.Stat(filepath.Dir(jailRoot(p))); err == nil {
		_ = exec.Command("sudo", "rm", "-rf", "--one-file-system", filepath.Dir(jailRoot(p))).Run()
	}
	if _, err := os.Stat(cgroupDir(p)); err != nil {
		return
	}
	for i := 0; i < 20; i++ { // the cgroup empties a moment after the process dies
		if err := exec.Command("sudo", "rmdir", cgroupDir(p)).Run(); err == nil {
			break
		}
		if _, err := os.Stat(cgroupDir(p)); errors.Is(err, os.ErrNotExist) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	removeJail(p)
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
