package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrioid/statesman"
)

func loadFixture(t *testing.T, name string) (*statesman.Definition, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return Load(data)
}

func mustLoad(t *testing.T, name string) *statesman.Definition {
	t.Helper()
	def, err := loadFixture(t, name)
	if err != nil {
		t.Fatalf("Load(%s) unexpected error: %v", name, err)
	}
	return def
}

func TestLoadOrder(t *testing.T) {
	def := mustLoad(t, "order.json")

	if def.ID != "order" {
		t.Errorf("Definition.ID = %q, want %q", def.ID, "order")
	}
	if def.Root.Kind != statesman.StateCompound {
		t.Errorf("Root.Kind = %v, want compound", def.Root.Kind)
	}
	if def.Root.Initial == nil || def.Root.Initial.Key != "idle" {
		t.Fatalf("Root.Initial = %v, want child %q", def.Root.Initial, "idle")
	}

	processing := def.Lookup("processing")
	if processing == nil {
		t.Fatal("Lookup(processing) = nil")
	}
	if processing.Kind != statesman.StateCompound {
		t.Errorf("processing.Kind = %v, want compound", processing.Kind)
	}
	if processing.Initial == nil || processing.Initial.Key != "charging" {
		t.Fatalf("processing.Initial = %v, want child %q", processing.Initial, "charging")
	}

	charging := def.Lookup("processing.charging")
	if charging == nil {
		t.Fatal("Lookup(processing.charging) = nil")
	}

	// Invoke: exactly one, id/src as authored.
	if len(charging.Invokes) != 1 {
		t.Fatalf("charging.Invokes len = %d, want 1", len(charging.Invokes))
	}
	inv := charging.Invokes[0]
	if inv.ID != "charge" || inv.Src != "chargeCard" {
		t.Errorf("invoke = {ID:%q Src:%q}, want {charge chargeCard}", inv.ID, inv.Src)
	}

	// onDone -> confirming.
	if len(inv.OnDone) != 1 || len(inv.OnDone[0].Targets) != 1 {
		t.Fatalf("invoke.OnDone shape = %v", inv.OnDone)
	}
	if got := inv.OnDone[0].Targets[0]; got != def.Lookup("confirming") {
		t.Errorf("OnDone[0].Targets[0] = %q, want confirming", got.ID)
	}

	// onError: two transitions; first guarded + incrementRetries -> retrying;
	// second -> errored.
	if len(inv.OnError) != 2 {
		t.Fatalf("invoke.OnError len = %d, want 2", len(inv.OnError))
	}
	e0 := inv.OnError[0]
	if e0.Guard == nil || e0.Guard.Type != "hasRetriesLeft" {
		t.Errorf("OnError[0].Guard = %v, want hasRetriesLeft", e0.Guard)
	}
	if len(e0.Actions) != 1 || e0.Actions[0].Type != "incrementRetries" {
		t.Errorf("OnError[0].Actions = %v, want [incrementRetries]", e0.Actions)
	}
	if len(e0.Targets) != 1 || e0.Targets[0] != def.Lookup("retrying") {
		t.Errorf("OnError[0].Targets = %v, want [retrying]", e0.Targets)
	}
	e1 := inv.OnError[1]
	if e1.Guard != nil {
		t.Errorf("OnError[1].Guard = %v, want nil", e1.Guard)
	}
	if len(e1.Targets) != 1 || e1.Targets[0] != def.Lookup("errored") {
		t.Errorf("OnError[1].Targets = %v, want [errored]", e1.Targets)
	}

	// after 30000 with two candidates.
	var after30 []*statesman.Transition
	for _, tr := range charging.Transitions {
		if tr.IsAfter && tr.Delay.Millis == 30000 {
			after30 = append(after30, tr)
		}
	}
	if len(after30) != 2 {
		t.Fatalf("charging after 30000 candidates = %d, want 2", len(after30))
	}

	// retrying after with a symbolic delay.
	retrying := def.Lookup("retrying")
	if retrying == nil || len(retrying.Transitions) != 1 {
		t.Fatalf("retrying.Transitions = %v", retrying)
	}
	rt := retrying.Transitions[0]
	if !rt.IsAfter || rt.Delay.Symbol != "RetryDelay" || rt.Delay.Millis != 0 {
		t.Errorf("retrying after delay = %+v, want Symbol=RetryDelay", rt.Delay)
	}

	// final states.
	if d := def.Lookup("done"); d == nil || d.Kind != statesman.StateFinal {
		t.Errorf("done.Kind = %v, want final", d)
	}
	if e := def.Lookup("errored"); e == nil || e.Kind != statesman.StateFinal {
		t.Errorf("errored.Kind = %v, want final", e)
	}

	// callsite totals.
	if def.ActionCount <= 0 || def.GuardCount <= 0 {
		t.Errorf("ActionCount=%d GuardCount=%d, want both > 0", def.ActionCount, def.GuardCount)
	}

	assertCallsitesUniqueContiguous(t, def)
}

// assertCallsitesUniqueContiguous walks every action/guard ref and verifies the
// callsite ids form contiguous 0..N-1 sequences with no duplicates.
func assertCallsitesUniqueContiguous(t *testing.T, def *statesman.Definition) {
	t.Helper()
	var actions, guards []int

	collect := func(t *statesman.Transition) {
		if t.Guard != nil {
			guards = append(guards, t.Guard.Callsite)
		}
		for _, a := range t.Actions {
			actions = append(actions, a.Callsite)
		}
	}

	var walk func(n *statesman.StateNode)
	walk = func(n *statesman.StateNode) {
		for _, a := range n.Entry {
			actions = append(actions, a.Callsite)
		}
		for _, tr := range n.Transitions {
			collect(tr)
		}
		for _, a := range n.Exit {
			actions = append(actions, a.Callsite)
		}
		for _, iv := range n.Invokes {
			for _, tr := range iv.OnDone {
				collect(tr)
			}
			for _, tr := range iv.OnError {
				collect(tr)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(def.Root)

	checkSeq(t, "action", actions, def.ActionCount)
	checkSeq(t, "guard", guards, def.GuardCount)
}

func checkSeq(t *testing.T, label string, ids []int, count int) {
	t.Helper()
	if len(ids) != count {
		t.Errorf("%s callsite usage = %d refs, but %sCount = %d", label, len(ids), label, count)
	}
	seen := make([]bool, count)
	for _, id := range ids {
		if id < 0 || id >= count {
			t.Errorf("%s callsite id %d out of range [0,%d)", label, id, count)
			continue
		}
		if seen[id] {
			t.Errorf("%s callsite id %d assigned more than once", label, id)
		}
		seen[id] = true
	}
	for i, ok := range seen {
		if !ok {
			t.Errorf("%s callsite id %d never assigned (not contiguous)", label, i)
		}
	}
}

func TestDocumentOrderPreserved(t *testing.T) {
	// Siblings declared in reverse-alphabetical order: a correct token-stream
	// loader keeps declaration order; a map-based one would reorder them.
	src := `{
		"id": "m",
		"initial": "zebra",
		"states": {
			"zebra": {},
			"apple": {},
			"mango": {}
		}
	}`
	def, err := Load([]byte(src))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantKeys := []string{"zebra", "apple", "mango"}
	if len(def.Root.Children) != len(wantKeys) {
		t.Fatalf("Root.Children len = %d, want %d", len(def.Root.Children), len(wantKeys))
	}
	for i, want := range wantKeys {
		if got := def.Root.Children[i].Key; got != want {
			t.Errorf("Children[%d].Key = %q, want %q (declaration order not preserved)", i, got, want)
		}
	}

	// DocOrder is pre-order: root=0, then children in declaration order.
	if def.Root.DocOrder != 0 {
		t.Errorf("Root.DocOrder = %d, want 0", def.Root.DocOrder)
	}
	prev := def.Root.DocOrder
	for i, c := range def.Root.Children {
		if c.DocOrder <= prev {
			t.Errorf("Children[%d] (%q) DocOrder = %d not ascending after %d", i, c.Key, c.DocOrder, prev)
		}
		prev = c.DocOrder
	}
}

func TestRejectionFixtures(t *testing.T) {
	cases := []struct {
		fixture string
		want    string // substring the error must contain
	}{
		{"no-invoke-id.json", "invoke-id-required"},
		{"compound-no-initial.json", "compound-requires-initial"},
		{"partial-wildcard.json", "event-descriptor-wildcard"},
		{"bad-target.json", "target-resolution"},
		{"unknown-field.json", "unknown field"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			def, err := loadFixture(t, tc.fixture)
			if err == nil {
				t.Fatalf("Load(%s) = %v, want error", tc.fixture, def)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load(%s) error = %q, want substring %q", tc.fixture, err.Error(), tc.want)
			}
		})
	}
}

func TestRootSchemaKeyAccepted(t *testing.T) {
	// Stately Studio exports carry a top-level "$schema" meta-keyword so editors
	// can suggest fields and validate; the loader accepts and ignores it.
	src := `{
		"$schema": "https://raw.githubusercontent.com/statelyai/schema/refs/heads/main/machineSchema.json",
		"id": "m",
		"initial": "idle",
		"states": { "idle": {} }
	}`
	def, err := Load([]byte(src))
	if err != nil {
		t.Fatalf("Load with root $schema: %v", err)
	}
	if def.ID != "m" {
		t.Errorf("ID = %q, want m", def.ID)
	}
}

func TestNestedSchemaKeyRejected(t *testing.T) {
	// $schema is a document-root meta-keyword; inside a state node it is
	// meaningless and must be rejected like any other unknown field.
	src := `{
		"id": "m",
		"initial": "idle",
		"states": { "idle": { "$schema": "x" } }
	}`
	if _, err := Load([]byte(src)); err == nil {
		t.Fatal("nested $schema should be rejected, got nil error")
	}
}
