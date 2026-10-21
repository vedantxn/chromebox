package blockdiff

import (
	"bytes"
	"crypto/sha256"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This test needs a filesystem with reflink and FIEMAP support (XFS or btrfs).
// Point CHROMEBOX_TEST_DIR at a directory on one; it is skipped otherwise.
func TestRealFilesystemRoundTrip(t *testing.T) {
	base := os.Getenv("CHROMEBOX_TEST_DIR")
	if base == "" {
		t.Skip("CHROMEBOX_TEST_DIR not set")
	}
	dir, err := os.MkdirTemp(base, "blockdiff-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	live, ref, copyPath := filepath.Join(dir, "live"), filepath.Join(dir, "ref"), filepath.Join(dir, "copy")

	// A 128 MiB image with data, and holes at 32-48 MiB and 100-110 MiB.
	const size = 128 << 20
	rng := rand.New(rand.NewSource(1))
	f, err := os.Create(live)
	if err != nil {
		t.Fatal(err)
	}
	chunk := make([]byte, 1<<20)
	for mb := 0; mb < size>>20; mb++ {
		if (mb >= 32 && mb < 48) || (mb >= 100 && mb < 110) {
			continue
		}
		rng.Read(chunk)
		if _, err := f.WriteAt(chunk, int64(mb)<<20); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Sync()

	// The reference is a reflink clone; the target starts as another clone of it,
	// standing in for the copy the migration pre-sends.
	for _, dst := range []string{ref, copyPath} {
		if out, err := exec.Command("cp", "--reflink=always", live, dst).CombinedOutput(); err != nil {
			t.Skipf("reflink not supported here: %v: %s", err, out)
		}
	}

	// The VM writes: tiny and unaligned, a megabyte, into a hole, a tail write.
	writes := []struct{ off, n int64 }{{100, 37}, {8 << 20, 1 << 20}, {40 << 20, 5000}, {104 << 20, 4096 * 3}, {size - 10, 10}, {70<<20 + 4095, 2}}
	for _, w := range writes {
		b := make([]byte, w.n)
		rng.Read(b)
		if _, err := f.WriteAt(b, w.off); err != nil {
			t.Fatal(err)
		}
	}
	f.Sync()
	f.Close()

	le, err := Extents(live)
	if err != nil {
		t.Fatal(err)
	}
	re, err := Extents(ref)
	if err != nil {
		t.Fatal(err)
	}
	ranges := Diff(le, re, size)
	total := Total(ranges)
	t.Logf("%d extents live, %d in reference; %d dirty ranges, %d KiB changed", len(le), len(re), len(ranges), total>>10)
	if total == 0 || total > size/4 {
		t.Fatalf("implausible delta of %d bytes", total)
	}

	src, _ := os.Open(live)
	defer src.Close()
	var stream bytes.Buffer
	if err := Send(&stream, src, ranges); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(&stream, copyPath); err != nil {
		t.Fatal(err)
	}
	if hash(t, live) != hash(t, copyPath) {
		t.Fatal("target differs from the live file after applying the delta")
	}
}

func hash(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}
