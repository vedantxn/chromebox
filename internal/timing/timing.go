// Package timing is a stopwatch with named laps. It answers "where did the
// milliseconds go": for boot now, and for migration downtime later.
package timing

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// Stopwatch records named laps relative to one start time.
type Stopwatch struct {
	start time.Time
	laps  []lap
}

type lap struct {
	name string
	at   time.Duration // since start
}

// Start starts the clock. time.Now carries Go's monotonic clock reading, so
// durations stay correct even if the wall clock is adjusted (e.g. by NTP).
func Start() *Stopwatch {
	return &Stopwatch{start: time.Now()}
}

// Lap marks the end of a step.
func (s *Stopwatch) Lap(name string) {
	s.laps = append(s.laps, lap{name: name, at: time.Since(s.start)})
}

// Print writes one row per lap: how long that step took, and the running total.
func (s *Stopwatch) Print(w io.Writer) {
	fmt.Fprintf(w, "%-26s %10s %10s\n", "step", "step ms", "total ms")
	var prev time.Duration
	for _, l := range s.laps {
		fmt.Fprintf(w, "%-26s %10.1f %10.1f\n", l.name, ms(l.at-prev), ms(l.at))
		prev = l.at
	}
}

// record is the JSON shape saved per run, so many runs can be compared later.
type record struct {
	StartedAt time.Time   `json:"started_at"`
	TotalMs   float64     `json:"total_ms"`
	Laps      []lapRecord `json:"laps"`
}

type lapRecord struct {
	Name    string  `json:"name"`
	StepMs  float64 `json:"step_ms"`
	TotalMs float64 `json:"total_ms"`
}

// Save writes the laps to path as JSON.
func (s *Stopwatch) Save(path string) error {
	r := record{StartedAt: s.start}
	var prev time.Duration
	for _, l := range s.laps {
		r.Laps = append(r.Laps, lapRecord{Name: l.name, StepMs: ms(l.at - prev), TotalMs: ms(l.at)})
		prev = l.at
	}
	if n := len(s.laps); n > 0 {
		r.TotalMs = ms(s.laps[n-1].at)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
