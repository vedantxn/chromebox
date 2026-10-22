//go:build !linux

package vm

// Preallocate is an optimization for Linux nodes; elsewhere (tests on a
// laptop) the file is simply allocated when it is written.
func Preallocate(path string, size int64) error { return nil }

// PunchZeros is a Linux tmpfs optimization; elsewhere it frees nothing.
func PunchZeros(path string) (int64, error) { return 0, nil }
