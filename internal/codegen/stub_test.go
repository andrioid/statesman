package codegen

import (
	"os"
	"strings"
	"testing"

	"github.com/andrioid/statesman/schema"
)

// TestStubFilenamesAndBehavior checks that stub output lands in the de-jargoned,
// machine-prefixed files and that the Impl behavior skeleton carries one
// panicking method per action/guard callsite with the concrete event type.
func TestStubFilenamesAndBehavior(t *testing.T) {
	def, err := schema.Load([]byte(`{
		"id": "light",
		"initial": "off",
		"states": {
			"off": {"on": {"FLIP": {"target": "on", "actions": [{"type": "recordFlip"}]}}},
			"on": {"on": {"FLIP": {"target": "off", "guard": {"type": "canFlip"}}}}
		}
	}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Pkg is nil: nothing resolves yet, so stub owes the full cold-start set.
	res := &Resolution{
		Events: map[string]*EventSym{},
		Actors: map[string]*ActorSym{},
		Delays: map[string]bool{},
		Unresolved: []Unresolved{
			{Kind: "event", GoName: "Flip", Detail: "FLIP"},
			{Kind: "context", GoName: "ContextFields"},
		},
	}
	out := Stub(res, def)

	events, ok := out["light.events.go"]
	if !ok {
		t.Fatalf("no light.events.go in %v", keys(out))
	}
	for _, want := range []string{"type Event interface", "lightEvent()", "type Flip struct{}", "type ContextFields struct"} {
		if !strings.Contains(events, want) {
			t.Errorf("light.events.go missing %q:\n%s", want, events)
		}
	}

	behavior, ok := out["light.behavior.go"]
	if !ok {
		t.Fatalf("no light.behavior.go in %v", keys(out))
	}
	for _, want := range []string{
		"type Impl struct{}",
		"func (Impl) RecordFlipOnFlip(ctx Context, evt Flip) ActionResult {",
		`panic("TODO: implement RecordFlipOnFlip")`,
		"func (Impl) CanFlipOnFlip(ctx Context, evt Flip) bool {",
		`panic("TODO: implement CanFlipOnFlip")`,
	} {
		if !strings.Contains(behavior, want) {
			t.Errorf("light.behavior.go missing %q:\n%s", want, behavior)
		}
	}

	// No invokes or after delays -> no actors/delays files.
	if _, ok := out["light.actors.go"]; ok {
		t.Errorf("unexpected light.actors.go: %q", out["light.actors.go"])
	}
	if _, ok := out["light.delays.go"]; ok {
		t.Errorf("unexpected light.delays.go: %q", out["light.delays.go"])
	}
}

// TestStubSkipsImplementedBehavior proves the skeleton is strictly additive: the
// simple example already implements every Implementations method, so stub emits
// no behavior file for it.
func TestStubSkipsImplementedBehavior(t *testing.T) {
	dir := "../../examples/simple"
	data, err := os.ReadFile(dir + "/simple.machine.json")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	def, err := schema.Load(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	res, err := Resolve(dir, def)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(res.Unresolved) != 0 {
		t.Fatalf("simple should resolve fully, got unresolved %+v", res.Unresolved)
	}
	out := Stub(res, def)
	if b, ok := out["simple.behavior.go"]; ok {
		t.Fatalf("behavior stub emitted though Impl implements all methods:\n%s", b)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
