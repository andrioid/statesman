package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrioid/statesman/schema"
	"golang.org/x/tools/go/packages"
)

// TestGenerateOrderCompiles generates order.machine.gen.go for the order fixture and
// type-checks that the fixture + generated code form a valid package wired to the
// runtime core.
func TestGenerateOrderCompiles(t *testing.T) {
	dir := "testdata/orderpkg"
	data, err := os.ReadFile(filepath.Join(dir, "order.machine.json"))
	if err != nil {
		t.Fatalf("read machine.json: %v", err)
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
	src, err := Emit(res, def)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "order.machine.gen.go"), src, 0o644); err != nil {
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

// TestGuardFallbackEmitted: a guard type wired on 2+ callsites (hasRetriesLeft,
// on error.invoke.charge and the charging timeout) collapses to one context-only
// fallback in the interface, with per-callsite overrides preferred at dispatch.
func TestGuardFallbackEmitted(t *testing.T) {
	dir := "testdata/orderpkg"
	data, err := os.ReadFile(filepath.Join(dir, "order.machine.json"))
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
	src, err := Emit(res, def)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	got := string(src)

	iface := got[strings.Index(got, "type Implementations interface"):]
	iface = iface[:strings.Index(iface, "}")]
	if !strings.Contains(iface, "HasRetriesLeft(ctx Context) bool") {
		t.Fatalf("interface missing context-only fallback HasRetriesLeft:\n%s", iface)
	}
	if strings.Contains(iface, "HasRetriesLeftOnChargeError") || strings.Contains(iface, "HasRetriesLeftOnProcessingCharging") {
		t.Fatalf("interface must not require per-callsite guard methods (they are optional overrides):\n%s", iface)
	}
	if !strings.Contains(got, "HasRetriesLeftOnChargeError(Context, ChargeError) bool") {
		t.Fatalf("dispatch missing override assertion for the error callsite:\n%s", got)
	}
	if !strings.Contains(got, "return impl.HasRetriesLeft(ctx)") {
		t.Fatalf("dispatch missing fallback call:\n%s", got)
	}
}
