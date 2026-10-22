package pageserver

import (
	"bytes"
	"math/rand"
	"testing"
	"time"
)

// A memory image with zero pages, single non-zero pages and long runs, some
// crossing a chunk boundary: the target must end up with exactly the same
// bytes from the stream alone, and faults must return the right bytes.
func TestStreamAndFetch(t *testing.T) {
	mem := make([]byte, 3*chunk)
	rng := rand.New(rand.NewSource(1))
	fill := func(from, to int) { rng.Read(mem[from:to]) }
	fill(0, pageSize)                        // first page
	fill(5*pageSize, 6*pageSize)             // a lone page
	fill(chunk-3*pageSize, chunk+4*pageSize) // a run across a chunk boundary
	fill(2*chunk+100*pageSize, 3*chunk)      // a long run to the end

	s, err := Listen("127.0.0.1", "secret", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Start(mem)

	c, err := Dial(s.Addr(), "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	buf := make([]byte, 16*pageSize)
	off := uint64(chunk - 8*pageSize)
	if err := c.Fetch(off, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, mem[off:off+uint64(len(buf))]) {
		t.Fatal("fetched bytes differ")
	}

	got := make([]byte, len(mem))
	var sent int
	zeros := 0
	if err := c.Stream(func(off uint64, data []byte) error {
		copy(got[off:], data)
		sent += len(data)
		return nil
	}, func(off, n uint64) error {
		zeros += int(n)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, mem) {
		t.Fatal("streamed memory differs")
	}
	if want := pageSize + pageSize + 7*pageSize + (chunk - 100*pageSize); sent != want {
		t.Fatalf("streamed %d bytes, want only the non-zero %d", sent, want)
	}
	if sent+zeros != len(mem) {
		t.Fatalf("non-zero %d + zero %d bytes do not cover the %d-byte memory", sent, zeros, len(mem))
	}
	if err := c.Ack(); err != nil {
		t.Fatal(err)
	}
	if err := s.Wait(2 * time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestWrongTokenIsRefused(t *testing.T) {
	mem := make([]byte, chunk)
	mem[0] = 1
	s, _ := Listen("127.0.0.1", "secret", "")
	defer s.Close()
	s.Start(mem)
	c, err := Dial(s.Addr(), "wrong")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Fetch(0, make([]byte, pageSize)); err == nil {
		t.Fatal("a wrong token got memory")
	}
}

// Serving from a file (pread) must produce exactly what serving from memory does.
func TestWriteRunsFromMatchesWriteRuns(t *testing.T) {
	mem := make([]byte, 3*chunk+5*pageSize)
	rng := rand.New(rand.NewSource(2))
	rng.Read(mem[chunk-pageSize : chunk+2*pageSize])
	rng.Read(mem[3*chunk+pageSize : 3*chunk+3*pageSize])
	var a, b bytes.Buffer
	if err := WriteRuns(&a, mem, nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteRunsFrom(&b, bytes.NewReader(mem), int64(len(mem)), nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("WriteRunsFrom differs from WriteRuns")
	}
}
