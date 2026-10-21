package orchestrator

import (
	"fmt"
	"sort"
	"strings"
)

// NodeView is what the scheduler knows about a node when it decides.
type NodeView struct {
	Name       string
	Up         bool
	Cordoned   bool
	CapMemMiB  int               // memory VMs may use on the node
	UsedMemMiB int               // memory of the VMs already assigned to it
	Bases      map[string]string // base name -> disk checksum the node holds
}

func (n NodeView) free() int { return n.CapMemMiB - n.UsedMemMiB }

// Request describes the VM to place.
type Request struct {
	MemMiB  int
	Base    string
	BaseSHA string // if set, the node must hold exactly this version of the base
}

// Strategy chooses among the nodes that can take the VM.
type Strategy string

const (
	// Spread picks the node with the most free memory. A failed node then loses
	// as few VMs as possible, and load stays even.
	Spread Strategy = "spread"
	// Pack picks the node with the least free memory that still fits (best
	// fit). It keeps whole nodes empty, which makes them cheap to switch off.
	Pack Strategy = "pack"
)

// Place chooses a node for req, skipping any named in exclude (a drain excludes
// the node it is emptying). It is a pure function of its arguments so that its
// behaviour can be tested exhaustively.
func Place(nodes []NodeView, req Request, strat Strategy, exclude ...string) (string, error) {
	skip := map[string]bool{}
	for _, e := range exclude {
		skip[e] = true
	}
	var ok []NodeView
	var why []string
	for _, n := range nodes {
		switch {
		case skip[n.Name]:
			why = append(why, n.Name+" excluded")
		case !n.Up:
			why = append(why, n.Name+" down")
		case n.Cordoned:
			why = append(why, n.Name+" cordoned")
		case req.Base != "" && !holds(n, req):
			why = append(why, n.Name+" lacks base "+req.Base)
		case n.free() < req.MemMiB:
			why = append(why, fmt.Sprintf("%s has %d MiB free", n.Name, n.free()))
		default:
			ok = append(ok, n)
		}
	}
	if len(ok) == 0 {
		sort.Strings(why)
		return "", fmt.Errorf("no node can take a %d MiB VM (%s)", req.MemMiB, strings.Join(why, ", "))
	}
	sort.Slice(ok, func(i, j int) bool {
		if ok[i].free() != ok[j].free() {
			if strat == Pack {
				return ok[i].free() < ok[j].free()
			}
			return ok[i].free() > ok[j].free()
		}
		return ok[i].Name < ok[j].Name // a tie is broken by name, so the choice is repeatable
	})
	return ok[0].Name, nil
}

func holds(n NodeView, req Request) bool {
	sha, has := n.Bases[req.Base]
	return has && (req.BaseSHA == "" || sha == req.BaseSHA)
}
