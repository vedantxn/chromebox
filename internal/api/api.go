// Package api holds the messages exchanged between the orchestrator, the node
// agents and the ctl client.
package api

import "github.com/vedantxn/chromebox/internal/timing"

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
	MemMiB     int    `json:"mem_mib"`
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
