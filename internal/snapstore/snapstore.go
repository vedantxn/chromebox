// Package snapstore keeps VM snapshots in object storage (S3), so that a
// paused session or a saved profile can be started on any node, including one
// that did not exist when the snapshot was taken.
//
// A snapshot is a handful of objects under one prefix:
//
//	vmstate       Firecracker's device state (a few hundred KB)
//	disk          the VM's disk as the blocks that differ from DiskBase, the
//	              regular base every node holds (the extent diff of M2): a few MiB
//	mem/0000 ...  the guest's memory as runs of non-zero pages (the post-copy
//	              stream format), cut into 32 MiB ranges that are written and
//	              read in parallel; zero pages cost nothing
//	manifest.json written last, so a half-written snapshot is never loaded
package snapstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/vedantxn/chromebox/internal/pageserver"
)

// Backend is the object store. S3 in production, memory in tests.
type Backend interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// SegmentBytes is how much guest memory one object covers. 32 MiB keeps each
// object a single PUT, and bounds how much a transfer holds in RAM at once:
// parallel x SegmentBytes.
const SegmentBytes = 32 << 20

// Manifest describes a stored snapshot.
type Manifest struct {
	Version   int       `json:"version"`
	Kind      string    `json:"kind"`      // "session" or "profile"
	Image     string    `json:"image"`     // the guest image (sets vCPUs, memory, the jail's limits)
	DiskBase  string    `json:"disk_base"` // the regular base the disk delta applies to
	MemMiB    int       `json:"mem_mib"`
	Segments  int       `json:"segments"`
	CreatedAt time.Time `json:"created_at"`
}

// Store reads and writes snapshots.
type Store struct {
	b        Backend
	Parallel int // concurrent transfers (default 16: resume download 1.2 s at 8, 0.9 s at 16, no better at 32)
}

func New(b Backend) *Store { return &Store{b: b, Parallel: 16} }

// Snapshot is what Save stores.
type Snapshot struct {
	Manifest   Manifest
	VMState    []byte
	Disk       []byte // blockdiff stream against Manifest.DiskBase
	Memory     io.ReaderAt
	MemorySize int64
}

// Stats says what a transfer moved.
type Stats struct {
	Objects   int     `json:"objects"`
	MemBytes  int64   `json:"mem_bytes"` // non-zero memory stored
	DiskBytes int64   `json:"disk_bytes"`
	Bytes     int64   `json:"bytes"` // everything
	Ms        float64 `json:"ms"`
}

func key(prefix, name string) string     { return prefix + name }
func segKey(prefix string, i int) string { return fmt.Sprintf("%smem/%04d", prefix, i) }

// parallel runs fn(0..n-1) with at most s.Parallel at a time and returns the
// first error (after the others finish).
func (s *Store) parallel(n int, fn func(i int) error) error {
	par := s.Parallel
	if par < 1 {
		par = 1
	}
	sem := make(chan struct{}, par)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(i); err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return first
}

// Save stores sn under prefix (which ends in "/"). On failure it removes what
// it wrote.
func (s *Store) Save(ctx context.Context, prefix string, sn Snapshot) (Stats, error) {
	start := time.Now()
	var st Stats
	var mu sync.Mutex
	add := func(n int, mem bool) {
		mu.Lock()
		st.Objects++
		st.Bytes += int64(n)
		if mem {
			st.MemBytes += int64(n)
		}
		mu.Unlock()
	}
	m := sn.Manifest
	m.Version = 1
	m.Segments = int((sn.MemorySize + SegmentBytes - 1) / SegmentBytes)
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	fail := func(err error) (Stats, error) {
		_, _ = s.b.DeletePrefix(context.Background(), prefix)
		return st, err
	}
	// vmstate and the disk delta are small: two objects, uploaded with the segments.
	extra := [][2]any{{"vmstate", sn.VMState}, {"disk", sn.Disk}}
	err := s.parallel(m.Segments+len(extra), func(i int) error {
		if i >= m.Segments {
			e := extra[i-m.Segments]
			if err := s.b.Put(ctx, key(prefix, e[0].(string)), e[1].([]byte)); err != nil {
				return err
			}
			add(len(e[1].([]byte)), false)
			if e[0] == "disk" {
				mu.Lock()
				st.DiskBytes = int64(len(sn.Disk))
				mu.Unlock()
			}
			return nil
		}
		off := int64(i) * SegmentBytes
		var buf bytes.Buffer
		if err := pageserver.WriteRunsRange(&buf, sn.Memory, off, min(SegmentBytes, sn.MemorySize-off), nil); err != nil {
			return err
		}
		n := buf.Len()
		if err := s.b.Put(ctx, segKey(prefix, i), buf.Bytes()); err != nil {
			return err
		}
		add(n, true)
		return nil
	})
	if err != nil {
		return fail(err)
	}
	mb, _ := json.Marshal(m)
	if err := s.b.Put(ctx, key(prefix, "manifest.json"), mb); err != nil {
		return fail(err)
	}
	add(len(mb), false)
	st.Ms = float64(time.Since(start).Microseconds()) / 1000
	return st, nil
}

// Loaded is what Load returns besides the memory, which goes to applyMem.
type Loaded struct {
	Manifest Manifest
	VMState  []byte
	Disk     []byte
	Stats    Stats
}

// Manifest reads just the manifest of the snapshot at prefix.
func (s *Store) Manifest(ctx context.Context, prefix string) (Manifest, error) {
	var m Manifest
	b, err := s.b.Get(ctx, key(prefix, "manifest.json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// Load fetches the snapshot at prefix. Memory arrives as runs of non-zero
// pages, read from every segment in parallel: applyMem(offset, data) is called
// for each (possibly from several goroutines at once, on disjoint ranges) and
// must not keep data. Zero pages are not sent: the caller's memory file
// starts as a hole.
func (s *Store) Load(ctx context.Context, prefix string, applyMem func(off uint64, data []byte) error) (Loaded, error) {
	start := time.Now()
	var out Loaded
	m, err := s.Manifest(ctx, prefix)
	if err != nil {
		return out, fmt.Errorf("snapshot %s: %w", prefix, err)
	}
	out.Manifest = m
	var mu sync.Mutex
	var bytesIn, memIn int64
	err = s.parallel(m.Segments+2, func(i int) error {
		switch {
		case i == m.Segments:
			b, err := s.b.Get(ctx, key(prefix, "vmstate"))
			out.VMState = b
			mu.Lock()
			bytesIn += int64(len(b))
			mu.Unlock()
			return err
		case i == m.Segments+1:
			b, err := s.b.Get(ctx, key(prefix, "disk"))
			out.Disk = b
			mu.Lock()
			bytesIn += int64(len(b))
			mu.Unlock()
			return err
		}
		b, err := s.b.Get(ctx, segKey(prefix, i))
		if err != nil {
			return err
		}
		mu.Lock()
		bytesIn += int64(len(b))
		memIn += int64(len(b))
		mu.Unlock()
		return pageserver.ReadRuns(bytes.NewReader(b), func(off uint64, data []byte) error { return applyMem(off, data) }, nil)
	})
	if err != nil {
		return out, err
	}
	out.Stats = Stats{Objects: m.Segments + 3, MemBytes: memIn, DiskBytes: int64(len(out.Disk)), Bytes: bytesIn,
		Ms: float64(time.Since(start).Microseconds()) / 1000}
	return out, nil
}

// Delete removes the snapshot at prefix and returns how many objects it held.
func (s *Store) Delete(ctx context.Context, prefix string) (int, error) {
	return s.b.DeletePrefix(ctx, prefix)
}
