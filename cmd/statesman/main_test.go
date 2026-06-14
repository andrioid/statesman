package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// watchFrame must never blank a valid diagram because of an in-progress edit:
// once a good render exists, a parse error keeps it on screen and appends the
// error rather than replacing it.
func TestWatchFrameKeepsLastGood(t *testing.T) {
	good := "machine\n├─ a\n└─ b"

	// First successful load: adopt it as the displayed and remembered render.
	frame, newGood, haveGood := watchFrame(good, nil, "", false)
	if frame != good || newGood != good || !haveGood {
		t.Fatalf("on success: frame=%q good=%q have=%v", frame, newGood, haveGood)
	}

	// Subsequent broken edit: keep the last good render, surface the error,
	// and do not lose the remembered render.
	parseErr := errors.New("line 14: unexpected }")
	frame, newGood, haveGood = watchFrame("", parseErr, newGood, haveGood)
	if !strings.Contains(frame, good) {
		t.Errorf("broken edit dropped the last good render:\n%s", frame)
	}
	if !strings.Contains(frame, parseErr.Error()) {
		t.Errorf("broken edit hid the error:\n%s", frame)
	}
	if newGood != good || !haveGood {
		t.Errorf("broken edit forgot the last good render: good=%q have=%v", newGood, haveGood)
	}
}

// Before any valid load, a parse error shows guidance and remembers nothing.
func TestWatchFrameNoGoodYet(t *testing.T) {
	frame, newGood, haveGood := watchFrame("", errors.New("boom"), "", false)
	if newGood != "" || haveGood {
		t.Errorf("should remember nothing: good=%q have=%v", newGood, haveGood)
	}
	if !strings.Contains(frame, "boom") {
		t.Errorf("error not shown: %q", frame)
	}
}

// resolveJSONPath accepts a machine.json directly or a directory holding exactly
// one, and enforces the one-machine-per-package rule with clear errors.
func TestResolveJSONPath(t *testing.T) {
	// A bare file path is returned unchanged.
	dir := t.TempDir()
	file := filepath.Join(dir, "a.machine.json")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveJSONPath(file); err != nil || got != file {
		t.Fatalf("file path: got %q err %v", got, err)
	}

	// A directory with exactly one *.machine.json resolves to it.
	one := t.TempDir()
	mj := filepath.Join(one, "checkout.machine.json")
	if err := os.WriteFile(mj, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveJSONPath(one); err != nil || got != mj {
		t.Fatalf("one-machine dir: got %q err %v", got, err)
	}

	// An empty directory is a clear error, not a silent empty path.
	if _, err := resolveJSONPath(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no *.machine.json") {
		t.Fatalf("empty dir: want 'no *.machine.json' error, got %v", err)
	}

	// Two machines in one directory violate one-machine-per-package.
	two := t.TempDir()
	for _, n := range []string{"a.machine.json", "b.machine.json"} {
		if err := os.WriteFile(filepath.Join(two, n), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resolveJSONPath(two); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("two machines: want 'multiple' error, got %v", err)
	}

	// A nonexistent path surfaces the stat error.
	if _, err := resolveJSONPath(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("nonexistent path: want error, got nil")
	}
}

// init scaffolds a package that builds with no hand-editing (the README's
// "compiles immediately" claim) and wires regeneration through the go tool.
func TestInitScaffoldCompiles(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	// A throwaway module that resolves the statesman runtime from local source,
	// so the scaffold generates and builds offline.
	mod := t.TempDir()
	gomod := fmt.Sprintf("module scaffoldtest\n\ngo 1.26\n\nrequire github.com/andrioid/statesman v0.0.0\n\nreplace github.com/andrioid/statesman => %s\n", repoRoot)
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	// Let `go list` (via go/packages) reconcile the pruned module graph offline.
	t.Setenv("GOFLAGS", "-mod=mod")

	pkgDir := filepath.Join(mod, "checkout")
	if err := initMachine(pkgDir); err != nil {
		t.Fatalf("initMachine: %v", err)
	}

	for _, f := range []string{"checkout.machine.json", "gen.go", "checkout.machine.gen.go"} {
		if _, err := os.Stat(filepath.Join(pkgDir, f)); err != nil {
			t.Errorf("scaffold missing %s: %v", f, err)
		}
	}

	// Regeneration is driven through the go tool, not a $PATH binary.
	gen, err := os.ReadFile(filepath.Join(pkgDir, "gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "//go:generate go tool statesman generate"; !strings.Contains(string(gen), want) {
		t.Errorf("gen.go missing %q:\n%s", want, gen)
	}

	build := exec.Command("go", "build", "./...")
	build.Dir = mod
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./...: %v\n%s", err, out)
	}
}
