// Package uffd serves a VM's memory page by page with Linux's userfaultfd.
//
// Firecracker can restore a snapshot without its memory: it maps empty memory
// for the guest, registers it with a userfaultfd (a file descriptor through
// which another process can own a range of memory), and hands that descriptor
// to a handler over a Unix socket. Each time the guest touches a page that is
// not there yet, the vCPU stops and the handler reads an event naming the
// page; it puts the page's bytes in place with one ioctl, and the vCPU goes on.
// That is what lets a migration resume the VM before its memory has arrived.
package uffd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Region is one block of guest memory, as Firecracker describes it: where it
// is mapped in Firecracker's address space, how big it is, and where its bytes
// start in the snapshot's memory file.
type Region struct {
	BaseHostVirtAddr uint64 `json:"base_host_virt_addr"`
	Size             uint64 `json:"size"`
	Offset           uint64 `json:"offset"`
	PageSize         uint64 `json:"page_size"`
}

// Receive accepts Firecracker's connection on ln and returns the userfaultfd
// and the memory layout it sends: one message, a JSON array of regions, with
// the descriptor attached as SCM_RIGHTS.
func Receive(ln *net.UnixListener) (int, []Region, error) {
	c, err := ln.AcceptUnix()
	if err != nil {
		return -1, nil, err
	}
	defer c.Close()
	buf := make([]byte, 4096)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, _, _, err := c.ReadMsgUnix(buf, oob)
	if err != nil {
		return -1, nil, err
	}
	msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(msgs) == 0 {
		return -1, nil, fmt.Errorf("no file descriptor from firecracker: %v", err)
	}
	fds, err := unix.ParseUnixRights(&msgs[0])
	if err != nil || len(fds) != 1 {
		return -1, nil, fmt.Errorf("bad file descriptor message: %v", err)
	}
	var regions []Region
	if err := json.Unmarshal(buf[:n], &regions); err != nil {
		return -1, nil, fmt.Errorf("memory layout %q: %w", buf[:n], err)
	}
	return fds[0], regions, nil
}

// The ioctls, as in <linux/userfaultfd.h>. Go's x/sys does not define them.
// Each number encodes the direction, the argument's size, the type 0xAA and
// the command: _IOWR(0xAA, 3, 40 bytes) is 0xC028AA03.
const (
	ioctlUnregister = 0x8010AA01 // _IOR(0xAA, 1, struct uffdio_range)
	ioctlWake       = 0x8010AA02 // _IOR(0xAA, 2, struct uffdio_range)
	ioctlCopy       = 0xC028AA03 // _IOWR(0xAA, 3, struct uffdio_copy)
	ioctlZeropage   = 0xC020AA04 // _IOWR(0xAA, 4, struct uffdio_zeropage)

	eventPagefault = 0x12
	msgSize        = 32 // sizeof(struct uffd_msg)
)

type uffdRange struct{ Start, Len uint64 }

type uffdCopy struct {
	Dst, Src, Len, Mode uint64
	Copied              int64 // out: bytes copied, or a negative errno
}

type uffdZeropage struct {
	Range  uffdRange
	Mode   uint64
	Zeroed int64
}

// ErrExist: the page is already there (another path installed it first).
var ErrExist = unix.EEXIST

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if e != 0 {
		return e
	}
	return nil
}

// Copy puts data at guest address dst (a page-aligned address in Firecracker's
// space) and wakes any vCPU waiting on it. The kernel copies the bytes out of
// our memory into Firecracker's, so data can be reused right after.
// It returns how many bytes were placed before an error.
func Copy(fd int, dst uint64, data []byte) (uint64, error) {
	a := uffdCopy{Dst: dst, Src: uint64(uintptr(unsafe.Pointer(&data[0]))), Len: uint64(len(data))}
	err := ioctl(fd, ioctlCopy, unsafe.Pointer(&a))
	if a.Copied > 0 {
		return uint64(a.Copied), err
	}
	return 0, err
}

// Zero maps zero pages over [dst, dst+n): cheaper than copying zeros.
func Zero(fd int, dst, n uint64) error {
	a := uffdZeropage{Range: uffdRange{dst, n}}
	return ioctl(fd, ioctlZeropage, unsafe.Pointer(&a))
}

// Wake restarts vCPUs waiting on [start, start+n), for when the page they
// wait for was installed by someone else.
func Wake(fd int, start, n uint64) error {
	r := uffdRange{start, n}
	return ioctl(fd, ioctlWake, unsafe.Pointer(&r))
}

// Unregister hands [start, start+n) back to the kernel: from then on a missing
// page there is an ordinary anonymous page, zero-filled on first touch. It
// acts on Firecracker's memory (the userfaultfd belongs to Firecracker's
// address space), and it wakes anyone still waiting in the range.
func Unregister(fd int, start, n uint64) error {
	r := uffdRange{start, n}
	return ioctl(fd, ioctlUnregister, unsafe.Pointer(&r))
}

// CopyRange installs data at dst page by page where needed: pages already
// present are skipped instead of failing the whole range.
func CopyRange(fd int, dst uint64, data []byte, pageSize uint64) error {
	for off := uint64(0); off < uint64(len(data)); {
		n, err := Copy(fd, dst+off, data[off:])
		off += n
		switch {
		case err == nil:
			return nil
		case errors.Is(err, unix.EEXIST):
			off += pageSize // that page is there already; go on after it
		case errors.Is(err, unix.EAGAIN):
			// The memory layout is changing (an unmap or remove event is
			// queued); try the same spot again.
		default:
			return err
		}
	}
	return nil
}

// Event is one page fault: the address the guest touched.
type Event struct {
	Addr uint64
}

// ReadEvents waits up to timeoutMs for events and returns the page faults
// among them. The userfaultfd is non-blocking (Firecracker creates it so), so
// poll says when there is something to read.
func ReadEvents(fd int, timeoutMs int, buf []byte) ([]Event, error) {
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(pfd, timeoutMs)
	if err != nil {
		if errors.Is(err, unix.EINTR) {
			return nil, nil
		}
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	got, err := unix.Read(fd, buf)
	if err != nil {
		if errors.Is(err, unix.EAGAIN) {
			return nil, nil
		}
		return nil, err
	}
	var evs []Event
	for i := 0; i+msgSize <= got; i += msgSize {
		m := buf[i : i+msgSize]
		if m[0] != eventPagefault {
			continue // remove/unmap events need a balloon, which these VMs don't have
		}
		evs = append(evs, Event{Addr: *(*uint64)(unsafe.Pointer(&m[16]))})
	}
	return evs, nil
}
