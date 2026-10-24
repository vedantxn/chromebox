package orchestrator

import (
	"errors"
	"testing"
	"time"

	"github.com/vedantxn/chromebox/internal/api"
)

// warmTest is an orchestrator with one open node and the store, enough for
// claiming; nothing talks to an agent.
func warmTest(t *testing.T) *Orchestrator {
	t.Helper()
	s := openTest(t)
	if err := s.UpsertNode("node-a", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{cfg: Config{HeartbeatTimeout: time.Minute, Warm: WarmConfig{PerNode: 1, Base: "chrome-v1"}}, store: s,
		seen: map[string]nodeState{"node-a": {Seen: time.Now(), Status: api.Status{CapMemMiB: 5000}}}, pending: map[string]int{},
		warmKick: make(chan struct{}, 1)}
	o.metrics = newMetrics(o)
	return o
}

func addWarm(t *testing.T, o *Orchestrator, id string) {
	t.Helper()
	v, err := o.store.InsertVM(id, "node-a", "chrome-v1", 1024, time.Now().Add(10*time.Minute))
	if err != nil || !o.store.casState(v.ID, StateCreating, StateWarm, v.Expires) {
		t.Fatalf("make %s warm: %v", id, err)
	}
}

func TestAWarmBrowserIsHandedOutOnce(t *testing.T) {
	o := warmTest(t)
	addWarm(t, o, "w1")
	vm, ok, err := o.claimWarm("chrome-v1", 15*time.Minute, nil)
	if err != nil || !ok || vm.ID != "w1" || vm.State != StateRunning {
		t.Fatalf("claim: %+v ok=%v err=%v", vm, ok, err)
	}
	if got, _ := o.store.GetVM("w1"); got.State != StateRunning || time.Until(got.ExpiresAt()) < 14*time.Minute {
		t.Fatalf("after claim: state %s, expires in %v", got.State, time.Until(got.ExpiresAt()))
	}
	if _, ok, _ := o.claimWarm("chrome-v1", 15*time.Minute, nil); ok {
		t.Fatal("the same warm browser was handed out twice")
	}
	if _, ok, _ := o.claimWarm("other-base", 15*time.Minute, nil); ok {
		t.Fatal("a warm browser of another base was handed out")
	}
}

func TestAWarmBrowserGoesBackWhenTheAccountIsRefused(t *testing.T) {
	o := warmTest(t)
	addWarm(t, o, "w1")
	refuse := func(string) error { return errors.New("limit: at most 2 sessions at a time") }
	if _, ok, err := o.claimWarm("chrome-v1", 15*time.Minute, []func(string) error{refuse}); ok || err == nil {
		t.Fatalf("claim should fail with the limit: ok=%v err=%v", ok, err)
	}
	if got, _ := o.store.GetVM("w1"); got.State != StateWarm {
		t.Fatalf("refused claim left it %s, want warm", got.State)
	}
}

func TestWarmBrowsersOnACordonedNodeAreNotHandedOut(t *testing.T) {
	o := warmTest(t)
	addWarm(t, o, "w1")
	_ = o.store.SetCordoned("node-a", true)
	if _, ok, _ := o.claimWarm("chrome-v1", 15*time.Minute, nil); ok {
		t.Fatal("handed out a warm browser on a cordoned node")
	}
}

func TestWarmBrowsersDoNotCountAgainstTheVMCap(t *testing.T) {
	vms := []api.VM{{State: StateRunning}, {State: StateWarm}, {State: StateWarming}, {State: StatePaused}}
	if n := countLive(vms); n != 1 {
		t.Fatalf("countLive = %d, want 1", n)
	}
}
