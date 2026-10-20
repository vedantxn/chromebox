package timing

import (
	"math"
	"slices"
	"time"
)

// Summary describes many runs of the same measurement.
type Summary struct {
	N             int
	P50, P95, Max time.Duration
}

// Summarize reports the median (what a typical run feels like), p95 (what the
// unlucky 1-in-20 run feels like) and the max. The fastest run is left out on
// purpose: nobody can count on getting it.
func Summarize(ds []time.Duration) Summary {
	s := slices.Clone(ds)
	slices.Sort(s)
	return Summary{N: len(s), P50: percentile(s, 0.50), P95: percentile(s, 0.95), Max: s[len(s)-1]}
}

// percentile uses the nearest-rank method on sorted data: the smallest value
// that at least p of the runs are at or below.
func percentile(sorted []time.Duration, p float64) time.Duration {
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(i, 0)]
}

// Steps returns each lap's own duration, keyed by lap name, plus the total.
func (s *Stopwatch) Steps() (steps map[string]time.Duration, total time.Duration) {
	steps = make(map[string]time.Duration, len(s.laps))
	var prev time.Duration
	for _, l := range s.laps {
		steps[l.name] = l.at - prev
		prev = l.at
	}
	return steps, prev
}

// Names returns the lap names in the order they were recorded.
func (s *Stopwatch) Names() []string {
	names := make([]string, len(s.laps))
	for i, l := range s.laps {
		names[i] = l.name
	}
	return names
}
