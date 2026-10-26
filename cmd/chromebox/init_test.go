package main

import (
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
)

// Every chromebox process pays its packages' init before main: the page-fault
// handler a migration starts inside its pause, too. A 28 ms argon2id hash in a
// package-level variable once made every migration's pause 43 ms longer. This
// test binary links the same packages, so its init is theirs.
func TestNoSlowPackageInit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "GODEBUG=inittrace=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	line := regexp.MustCompile(`(?m)^init (\S+) @[\d.]+ ms, ([\d.]+) ms clock`)
	found := line.FindAllSubmatch(out, -1)
	if len(found) == 0 {
		t.Fatalf("no init trace in:\n%s", out)
	}
	for _, m := range found {
		if ms, _ := strconv.ParseFloat(string(m[2]), 64); ms > 10 {
			t.Errorf("package %s takes %.1f ms to initialize; do the work on first use instead", m[1], ms)
		}
	}
}
