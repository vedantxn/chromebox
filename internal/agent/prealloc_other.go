//go:build !linux

package agent

// preallocate is an optimization for Linux nodes; elsewhere (tests on a
// laptop) the file is simply allocated when it is written.
func preallocate(path string, size int64) error { return nil }
