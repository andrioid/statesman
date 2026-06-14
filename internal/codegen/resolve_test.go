package codegen

import (
	"os"
	"testing"

	"github.com/andrioid/statesman/schema"
)

func TestResolveOrder(t *testing.T) {
	data, err := os.ReadFile("testdata/orderpkg/order.machine.json")
	if err != nil {
		t.Fatalf("read machine.json: %v", err)
	}
	def, err := schema.Load(data)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	res, err := Resolve("testdata/orderpkg", def)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(res.Unresolved) != 0 {
		t.Fatalf("unresolved set should be empty, got %+v", res.Unresolved)
	}
	for _, desc := range []string{"SUBMIT", "CONFIRM"} {
		if _, ok := res.Events[desc]; !ok {
			t.Errorf("event %q not resolved", desc)
		}
	}
	if res.ContextFields == nil {
		t.Error("ContextFields not resolved")
	}
	charge := res.Actors["chargeCard"]
	if charge == nil || charge.Kind != AdapterPromise {
		t.Fatalf("chargeCard = %+v, want promise", charge)
	}
	if charge.In == nil || charge.Out == nil {
		t.Errorf("promise should expose In/Out types, got In=%v Out=%v", charge.In, charge.Out)
	}
	inv := res.Actors["watchInventory"]
	if inv == nil || inv.Kind != AdapterCallback {
		t.Fatalf("watchInventory = %+v, want callback", inv)
	}
	if !res.Delays["RetryDelay"] {
		t.Error("RetryDelay symbolic delay not resolved")
	}
}

func TestResolveReportsUnresolved(t *testing.T) {
	// A machine referencing a missing event and actor.
	const mj = `{
		"id": "gap",
		"initial": "idle",
		"states": {
			"idle": {"on": {"GO": {"target": "busy"}}},
			"busy": {"invoke": [{"id": "w", "src": "doWork"}]}
		}
	}`
	def, err := schema.Load([]byte(mj))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	res, err := Resolve("testdata/orderpkg", def)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// GO event (Go) and doWork actor (DoWork) are not declared in orderpkg.
	kinds := map[string]string{}
	for _, u := range res.Unresolved {
		kinds[u.GoName] = u.Kind
	}
	if kinds["Go"] != "event" {
		t.Errorf("expected unresolved event Go, got %+v", res.Unresolved)
	}
	if kinds["DoWork"] != "actor" {
		t.Errorf("expected unresolved actor DoWork, got %+v", res.Unresolved)
	}
}
