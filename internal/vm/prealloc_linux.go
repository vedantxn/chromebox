package vm

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Preallocate gives path size bytes of real memory (it is on tmpfs) now, so
// that writing it later costs only the copy.
func Preallocate(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := unix.Fallocate(int(f.Fd()), 0, 0, size); err != nil {
		return fmt.Errorf("allocate %s: %w", path, err)
	}
	return nil
}

// PunchZeros frees the pages of path (a file in tmpfs) that hold only zeros,
// leaving holes that still read as zeros. A guest's memory is often 40% zero
// pages; a snapshot kept around (a fork image) need not hold them. It returns
// the bytes freed.
func PunchZeros(path string) (int64, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	mem, err := unix.Mmap(int(f.Fd()), 0, int(st.Size()), unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return 0, err
	}
	defer unix.Munmap(mem)
	const page = 4096
	var freed int64
	for i := 0; i < len(mem); {
		if !zeroPage(mem[i : i+page]) {
			i += page
			continue
		}
		j := i + page
		for j < len(mem) && zeroPage(mem[j:j+page]) {
			j += page
		}
		if err := unix.Fallocate(int(f.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, int64(i), int64(j-i)); err != nil {
			return freed, err
		}
		freed += int64(j - i)
		i = j
	}
	return freed, nil
}

func zeroPage(b []byte) bool {
	for len(b) >= 8 {
		if *(*uint64)(unsafe.Pointer(&b[0])) != 0 {
			return false
		}
		b = b[8:]
	}
	return true
}
