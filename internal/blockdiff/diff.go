package blockdiff

import "sort"

// Range is a span of bytes in a file.
type Range struct {
	Offset, Length uint64
}

// Diff returns the byte ranges of a file whose storage differs between two
// extent maps of the same logical file: live (the VM's disk now) and ref (the
// reference clone made earlier). A range is unchanged if both maps place it at
// the same device address, or both leave it unmapped (zeros). size is the
// file's length. The result is sorted, with adjacent ranges merged.
func Diff(live, ref []Extent, size uint64) []Range {
	// Cut points: every place either map changes segment. Between two
	// consecutive cut points each map is uniform (one extent, or a hole).
	cuts := map[uint64]struct{}{0: {}, size: {}}
	for _, e := range live {
		cuts[e.Logical] = struct{}{}
		cuts[e.Logical+e.Length] = struct{}{}
	}
	for _, e := range ref {
		cuts[e.Logical] = struct{}{}
		cuts[e.Logical+e.Length] = struct{}{}
	}
	points := make([]uint64, 0, len(cuts))
	for c := range cuts {
		if c <= size {
			points = append(points, c)
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i] < points[j] })

	var out []Range
	li, ri := 0, 0
	for k := 0; k+1 < len(points); k++ {
		pos, end := points[k], points[k+1]
		for li < len(live) && live[li].Logical+live[li].Length <= pos {
			li++
		}
		for ri < len(ref) && ref[ri].Logical+ref[ri].Length <= pos {
			ri++
		}
		pl, okl := physicalAt(live, li, pos)
		pr, okr := physicalAt(ref, ri, pos)
		if okl == okr && (!okl || pl == pr) {
			continue // same storage, or both zeros
		}
		if n := len(out); n > 0 && out[n-1].Offset+out[n-1].Length == pos {
			out[n-1].Length += end - pos
		} else {
			out = append(out, Range{Offset: pos, Length: end - pos})
		}
	}
	return out
}

// physicalAt is the device address of byte pos, if the extent at index i
// covers it.
func physicalAt(exts []Extent, i int, pos uint64) (uint64, bool) {
	if i < len(exts) && exts[i].Logical <= pos {
		return exts[i].Physical + (pos - exts[i].Logical), true
	}
	return 0, false
}

// Total is the number of bytes in the ranges.
func Total(rs []Range) uint64 {
	var n uint64
	for _, r := range rs {
		n += r.Length
	}
	return n
}
