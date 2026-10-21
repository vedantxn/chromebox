// Package remote runs commands on another machine over ssh, through one shared
// connection. Opening an ssh connection costs a TCP handshake plus a key
// exchange (tens of milliseconds); sharing one means that cost is paid once,
// before anything time-sensitive happens, and later commands only pay a round
// trip.
package remote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Remote is an ssh target such as ec2-user@172.31.2.181.
type Remote struct {
	Host string
	Opts []string // ssh options, shared by every command and by rsync/tar pipes
}

// New returns a Remote that authenticates with the node-to-node key
// (~/.ssh/chromebox_node) and multiplexes all commands over one connection.
func New(host string) *Remote {
	home, _ := os.UserHomeDir()
	return &Remote{Host: host, Opts: []string{
		"-i", filepath.Join(home, ".ssh", "chromebox_node"),
		"-o", "IdentitiesOnly=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=/tmp/chromebox-ssh-%C",
		"-o", "ControlPersist=60",
	}}
}

// Run executes a command on the target and returns its stdout.
func (r *Remote) Run(ctx context.Context, argv ...string) ([]byte, error) {
	args := append(append([]string{}, r.Opts...), r.Host)
	args = append(args, argv...)
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh %s %v: %w: %s", r.Host, argv, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

// SSHCmd is the ssh command line rsync should use, sharing our connection.
func (r *Remote) SSHCmd() string {
	return "ssh " + strings.Join(r.Opts, " ")
}

// Command builds (but does not run) an ssh command for the target, for callers
// that need to wire up stdin/stdout themselves.
func (r *Remote) Command(ctx context.Context, argv ...string) *exec.Cmd {
	args := append(append([]string{}, r.Opts...), r.Host)
	return exec.CommandContext(ctx, "ssh", append(args, argv...)...)
}
