// Package api holds the messages exchanged between the orchestrator, the node
// agents and the ctl client.
package api

import (
	"time"

	"github.com/vedantxn/chromebox/internal/timing"
)

// VMInfo is one VM as an agent reports it.
type VMInfo struct {
	ID       string `json:"id"`
	Image    string `json:"image"`
	Base     string `json:"base,omitempty"`
	NetIndex int    `json:"net_index"`
	MemMiB   int    `json:"mem_mib"`
	// PssMiB is the memory this VM's Firecracker process really costs the node:
	// its private pages plus its share of pages it has in common with other VMs
	// (the untouched memory of the base snapshot).
	PssMiB float64 `json:"pss_mib"`
	// PrivateMiB is the part only this VM has: what it wrote since it was cloned.
	PrivateMiB float64 `json:"private_mib"`
}

// BaseInfo is a base snapshot a node holds.
type BaseInfo struct {
	Name       string `json:"name"`
	DiskSHA256 string `json:"disk_sha256"`
	MemMiB     int    `json:"mem_mib"`  // memory of a VM made from it
	HeldMiB    int    `json:"held_mib"` // RAM the base itself holds on this node
	// Fork images (M6): made from a running VM, kept while VMs use them.
	ForkOf   string    `json:"fork_of,omitempty"`
	DiskBase string    `json:"disk_base,omitempty"` // the regular base a fork image's disk descends from
	BuiltAt  time.Time `json:"built_at"`
}

// ForkImageRequest asks an agent to turn a running VM into a fork image.
type ForkImageRequest struct {
	Image string `json:"image"`
}

// PauseRequest asks an agent to snapshot a running VM into object storage
// under Prefix and stop it.
type PauseRequest struct {
	Prefix string `json:"prefix"`
}

// PauseResponse: what pausing took and stored.
type PauseResponse struct {
	Laps      timing.Record `json:"laps"`
	PausedMs  float64       `json:"paused_ms"` // the guest, while its snapshot was taken
	UploadMs  float64       `json:"upload_ms"` // the snapshot to storage
	TotalMs   float64       `json:"total_ms"`
	StoredMiB float64       `json:"stored_mib"` // everything stored
	MemoryMiB float64       `json:"memory_mib"` // non-zero memory stored
	DiskMiB   float64       `json:"disk_mib"`
	DiskBase  string        `json:"disk_base"` // the regular base the VM's disk descends from
}

// SaveProfileRequest asks an agent to store a running VM's state under Prefix,
// as a profile that new sessions can start from. The VM keeps running.
type SaveProfileRequest struct {
	Prefix string `json:"prefix"`
}

// SaveProfileResponse: what saving took and stored.
type SaveProfileResponse struct {
	PausedMs  float64 `json:"paused_ms"` // the running VM, while its state was captured
	UploadMs  float64 `json:"upload_ms"`
	StoredMiB float64 `json:"stored_mib"`
	MemMiB    int     `json:"mem_mib"`
	DiskBase  string  `json:"disk_base"`
}

// ProfileImageRequest asks an agent to have the profile snapshot at Prefix
// ready as a local base called Name (fork-p-...), fetching it if it is not here.
type ProfileImageRequest struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
}

// ProfileImageResponse: whether it had to be fetched, and what that took.
type ProfileImageResponse struct {
	Fetched    bool    `json:"fetched"`
	DownloadMs float64 `json:"download_ms"`
	StoredMiB  float64 `json:"stored_mib"`
	MemMiB     int     `json:"mem_mib"`
	DiskBase   string  `json:"disk_base"`
}

// ResumeRequest asks an agent to start a VM from the snapshot at Prefix, with
// network index NetIndex (its address does not change across a pause).
type ResumeRequest struct {
	Prefix   string `json:"prefix"`
	NetIndex int    `json:"net_index"`
}

// ResumeResponse: what resuming took.
type ResumeResponse struct {
	Laps       timing.Record `json:"laps"`
	DownloadMs float64       `json:"download_ms"`
	TotalMs    float64       `json:"total_ms"`
	StoredMiB  float64       `json:"stored_mib"`
}

// ForkImageResponse: how long the parent was paused, and the image's size.
type ForkImageResponse struct {
	Laps     timing.Record `json:"laps"`
	PausedMs float64       `json:"paused_ms"`
	HeldMiB  int           `json:"held_mib"`  // RAM the image holds, zero pages punched out
	ZeroMiB  int           `json:"zero_mib"`  // zero pages that were punched out
	DiskBase string        `json:"disk_base"` // the regular base its disk descends from
}

// PullImageResponse: what travelled to bring a fork image to another node.
type PullImageResponse struct {
	Laps    timing.Record `json:"laps"`
	DiskMiB float64       `json:"disk_mib"` // blocks that differ from the regular base
	MemMiB  float64       `json:"mem_mib"`  // non-zero memory
}

// Status is what an agent reports about its node: sent with every heartbeat and
// available at GET /v1/status.
type Status struct {
	Node      string `json:"node"`
	AgentPort int    `json:"agent_port"`
	VCPUs     int    `json:"vcpus"`
	// CapMemMiB is the memory VMs may use: the node's total minus what the host
	// and the bases held in RAM need.
	CapMemMiB int        `json:"cap_mem_mib"`
	VMs       []VMInfo   `json:"vms"`
	Bases     []BaseInfo `json:"bases"`
}

// CreateVMRequest asks an agent to start a VM from a base snapshot.
type CreateVMRequest struct {
	ID       string `json:"id"`
	Base     string `json:"base"`
	NetIndex int    `json:"net_index"`
}

// CreateVMResponse reports how the start went.
type CreateVMResponse struct {
	Laps timing.Record `json:"laps"` // the node's steps
	// OrchLaps are the orchestrator's own steps around it (set by the orchestrator).
	OrchLaps timing.Record `json:"orch_laps"`
}

// VM is a VM as the orchestrator knows it.
type VM struct {
	ID         string  `json:"id"`
	NetIndex   int     `json:"net_index"`
	Node       string  `json:"node"`
	Base       string  `json:"base"`
	State      string  `json:"state"`
	MemMiB     int     `json:"mem_mib"`
	CDPPath    string  `json:"cdp_path"`
	Created    string  `json:"created_at"`
	Expires    string  `json:"expires_at,omitempty"`
	PssMiB     float64 `json:"pss_mib,omitempty"`
	PrivateMiB float64 `json:"private_mib,omitempty"`
}

// Node is a node as the orchestrator knows it.
type Node struct {
	Name      string `json:"name"`
	Up        bool   `json:"up"`
	Cordoned  bool   `json:"cordoned"`
	VMs       int    `json:"vms"`
	CapMemMiB int    `json:"cap_mem_mib"`
	UsedMiB   int    `json:"used_mem_mib"`
	AgentURL  string `json:"agent_url"`
}

// MigrateRequest asks a source agent to move one of its VMs to another node.
type MigrateRequest struct {
	TargetAgent string `json:"target_agent"` // the target agent's URL
	TargetSSH   string `json:"target_ssh"`   // ssh target for the bulk data, e.g. ec2-user@10.0.0.5
	// StopAndCopy sends all memory during the pause (M2–M4), instead of
	// post-copy; kept to measure one against the other.
	StopAndCopy bool `json:"stop_and_copy,omitempty"`
	FaultKiB    int  `json:"fault_kib,omitempty"` // post-copy: bytes one fault fetches, in KiB (default 64)
}

// AcceptRequest tells the target how to get the memory it was not sent: from
// the source's page server (post-copy). Empty means the memory came with the
// snapshot.
type AcceptRequest struct {
	Source   string `json:"source,omitempty"` // host:port of the page server
	Token    string `json:"token,omitempty"`
	FaultKiB int    `json:"fault_kib,omitempty"`
}

// CommitResponse: what the source sent after the VM resumed on the target.
type CommitResponse struct {
	StreamMs  float64 `json:"stream_ms"` // target resumed -> it holds every page
	StreamMiB float64 `json:"stream_mib"`
}

// MigrateResponse: the VM now runs on the target and is paused on the source,
// waiting for commit (destroy the source copy) or abort (resume it).
type MigrateResponse struct {
	Laps       timing.Record `json:"laps"`
	TargetLaps timing.Record `json:"target_laps"`
	PausedMs   float64       `json:"paused_ms"` // source paused -> running on the target
	DeltaBytes int64         `json:"delta_bytes"`
	PostCopy   bool          `json:"post_copy"`
}

// IncomingRequest tells a target agent to get ready for a VM.
type IncomingRequest struct {
	ID       string `json:"id"`
	Base     string `json:"base"`
	NetIndex int    `json:"net_index"`
}

// ExpiresAt parses Expires; the zero time means "never".
func (v VM) ExpiresAt() time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v.Expires)
	return t
}
