package blockdiff

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	const size = 1000
	tests := []struct {
		name      string
		live, ref []Extent
		want      []Range
	}{
		{"identical maps", []Extent{{0, 5000, 400}}, []Extent{{0, 5000, 400}}, nil},
		{"identical but split differently",
			[]Extent{{0, 5000, 400}},
			[]Extent{{0, 5000, 100}, {100, 5100, 300}}, nil},
		{"one block rewritten elsewhere",
			[]Extent{{0, 5000, 100}, {100, 9000, 100}, {200, 5200, 200}},
			[]Extent{{0, 5000, 400}},
			[]Range{{100, 100}}},
		{"data written into a hole",
			[]Extent{{0, 5000, 100}, {300, 7000, 50}},
			[]Extent{{0, 5000, 100}},
			[]Range{{300, 50}}},
		{"hole punched",
			[]Extent{{0, 5000, 100}},
			[]Extent{{0, 5000, 100}, {300, 7000, 50}},
			[]Range{{300, 50}}},
		{"adjacent changes merge",
			[]Extent{{0, 9000, 50}, {50, 9500, 50}},
			[]Extent{{0, 5000, 100}},
			[]Range{{0, 100}}},
		{"both empty", nil, nil, nil},
	}
	for _, tt := range tests {
		got := Diff(tt.live, tt.ref, size)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSendApplyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	data := bytes.Repeat([]byte{0xAB}, 5000)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(src)
	defer f.Close()
	var stream bytes.Buffer
	ranges := []Range{{10, 100}, {4000, 900}}
	if err := Send(&stream, f, ranges); err != nil {
		t.Fatal(err)
	}
	n, err := Apply(&stream, dst)
	if err != nil || n != 1000 {
		t.Fatalf("applied %d bytes, err %v", n, err)
	}
	got, _ := os.ReadFile(dst)
	for i, b := range got {
		in := (i >= 10 && i < 110) || (i >= 4000 && i < 4900)
		if in && b != 0xAB || !in && b != 0 {
			t.Fatalf("byte %d = %#x", i, b)
		}
	}
}
