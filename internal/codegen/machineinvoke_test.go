package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrioid/statesman/schema"
	"golang.org/x/tools/go/packages"
)

// TestGenerateMachineInvokeCompiles: a machine invoke (fromMachine) is detected
// from `func() *statesman.Machine[CCtx, CEvt]`, wired with MachineActor in the
// generated constructor (done payload = child Context), and the child package is
// imported. The fixture + generated code must type-check together.
func TestGenerateMachineInvokeCompiles(t *testing.T) {
	dir := "testdata/submach"
	data, err := os.ReadFile(filepath.Join(dir, "parent.machine.json"))
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
		t.Fatalf("unresolved: %+v", res.Unresolved)
	}
	if a := res.Actors["runChild"]; a == nil || a.Kind != AdapterMachine {
		t.Fatalf("runChild = %+v, want a machine adapter", a)
	}

	src, err := Emit(res, def)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	got := string(src)
	if !strings.Contains(got, "statesman.MachineActor[Context, Event, child.Context, child.Event]") {
		t.Fatalf("missing MachineActor wiring with child type args:\n%s", got)
	}
	if !strings.Contains(got, "impl.RunChildInput") {
		t.Fatalf("machine invoke should be seeded via the RunChildInput mapper:\n%s", got)
	}
	if !strings.Contains(got, "type RunDone struct{ Output child.Context }") {
		t.Fatalf("done event should carry the child Context as Output:\n%s", got)
	}
	if !strings.Contains(got, `"github.com/andrioid/statesman/internal/codegen/testdata/submach/child"`) {
		t.Fatalf("missing child package import:\n%s", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "parent.machine.gen.go"), src, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var errs []string
	for _, p := range pkgs {
		for _, e := range p.Errors {
			errs = append(errs, e.Error())
		}
	}
	if len(errs) > 0 {
		t.Fatalf("generated package has type errors:\n%s\n\n--- generated ---\n%s", strings.Join(errs, "\n"), src)
	}
}
