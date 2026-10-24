package snapstore

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"testing"
)

// memBackend is an in-memory object store.
type memBackend struct {
	mu    sync.Mutex
	m     map[string][]byte
	failN int // fail the Nth Put (1-based), 0: never
	puts  int
}

func (b *memBackend) Put(_ context.Context, k string, d []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.puts++
	if b.failN > 0 && b.puts == b.failN {
		return errors.New("injected failure")
	}
	b.m[k] = append([]byte(nil), d...)
	return nil
}
func (b *memBackend) Get(_ context.Context, k string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.m[k]
	if !ok {
		return nil, errors.New("no such key " + k)
	}
	return d, nil
}
func (b *memBackend) DeletePrefix(_ context.Context, p string) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for k := range b.m {
		if strings.HasPrefix(k, p) {
			delete(b.m, k)
			n++
		}
	}
	return n, nil
}

// A memory image with zero pages, lone pages and long runs, 3 segments and a bit.
func image() []byte {
	mem := make([]byte, 3*SegmentBytes+5*4096)
	rng := rand.New(rand.NewSource(7))
	rng.Read(mem[0:4096])
	rng.Read(mem[SegmentBytes-4096 : SegmentBytes+8192]) // across a segment boundary
	rng.Read(mem[2*SegmentBytes+1<<20 : 2*SegmentBytes+9<<20])
	rng.Read(mem[len(mem)-4096:])
	return mem
}

func TestSaveLoadRoundTrip(t *testing.T) {
	b := &memBackend{m: map[string][]byte{}}
	s := New(b)
	mem := image()
	sn := Snapshot{Manifest: Manifest{Kind: "session", Image: "chrome", DiskBase: "chrome-v1", MemMiB: 97},
		VMState: []byte("vmstate bytes"), Disk: []byte("disk delta"), Memory: bytes.NewReader(mem), MemorySize: int64(len(mem))}
	st, err := s.Save(context.Background(), "sessions/x/1/", sn)
	if err != nil {
		t.Fatal(err)
	}
	if st.MemBytes >= int64(len(mem))/2 {
		t.Fatalf("stored %d bytes of a %d-byte image: zero pages should not be stored", st.MemBytes, len(mem))
	}
	got := make([]byte, len(mem))
	var mu sync.Mutex
	l, err := s.Load(context.Background(), "sessions/x/1/", func(off uint64, data []byte) error {
		mu.Lock()
		defer mu.Unlock()
		copy(got[off:], data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, mem) {
		t.Fatal("memory differs after a round trip")
	}
	if string(l.VMState) != "vmstate bytes" || string(l.Disk) != "disk delta" || l.Manifest.DiskBase != "chrome-v1" || l.Manifest.Segments != 4 {
		t.Fatalf("unexpected load: %+v", l.Manifest)
	}
}

func TestFailedSaveLeavesNothingAndNoManifest(t *testing.T) {
	b := &memBackend{m: map[string][]byte{}, failN: 3}
	s := New(b)
	mem := image()
	_, err := s.Save(context.Background(), "p/", Snapshot{Memory: bytes.NewReader(mem), MemorySize: int64(len(mem)), VMState: []byte("v"), Disk: []byte("d")})
	if err == nil {
		t.Fatal("expected the injected failure")
	}
	if len(b.m) != 0 {
		t.Fatalf("%d objects left behind after a failed save", len(b.m))
	}
	if _, err := s.Manifest(context.Background(), "p/"); err == nil {
		t.Fatal("a failed save left a manifest")
	}
}
