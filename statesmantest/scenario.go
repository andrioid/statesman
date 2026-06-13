package statesmantest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/schema"
	"golang.org/x/tools/txtar"
)

// scenarioEvent is a descriptor-only event for data-driven scenarios.
type scenarioEvent struct{ desc string }

func (e scenarioEvent) EventType() string { return e.desc }

type scenarioCtx struct{}

// RunScenarios discovers *.txtar scenario fixtures under dir and runs each as a
// subtest. Each archive holds a `machine.json` file and a `scenario` script; the
// runner drives the machine on virtual time with trivial appliers (guards true,
// actions noop), exercising the structural semantics (hierarchy, parallel,
// history, after, transitions). This is the curated behavioral corpus.
func RunScenarios(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.txtar"))
	if err != nil {
		t.Fatalf("glob scenarios: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no scenarios found in %s", dir)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".txtar")
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		t.Run(name, func(t *testing.T) { RunScenario(t, data) })
	}
}

// RunScenario executes one txtar scenario archive.
func RunScenario(t *testing.T, archive []byte) {
	t.Helper()
	ar := txtar.Parse(archive)
	var mjson, script []byte
	for _, f := range ar.Files {
		switch f.Name {
		case "machine.json":
			mjson = f.Data
		case "scenario":
			script = f.Data
		}
	}
	if mjson == nil || script == nil {
		t.Fatal("scenario must contain machine.json and scenario files")
	}
	def, err := schema.Load(mjson)
	if err != nil {
		t.Fatalf("load machine.json: %v", err)
	}
	apply := func(int, scenarioCtx, scenarioEvent) statesman.AppliedEffect[scenarioCtx] {
		return statesman.AppliedEffect[scenarioCtx]{Kind: statesman.EffectNoop}
	}
	guard := func(int, scenarioCtx, scenarioEvent) bool { return true }
	s := NewSync(func(o ...statesman.Option) *statesman.Machine[scenarioCtx, scenarioEvent] {
		return statesman.NewMachine[scenarioCtx, scenarioEvent](def, scenarioCtx{}, apply, guard, o...)
	})
	ctx := context.Background()
	if err := s.Start(ctx, "scenario"); err != nil {
		t.Fatalf("start: %v", err)
	}

	for i, raw := range strings.Split(string(script), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := runStep(t, s, line); err != nil {
			t.Fatalf("line %d (%q): %v", i+1, line, err)
		}
	}
	_ = s.M.Close()
}

func runStep(t *testing.T, s *Sync[scenarioCtx, scenarioEvent], line string) error {
	t.Helper()
	verb, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)
	ctx := context.Background()
	switch verb {
	case "send":
		return s.SendAndSettle(ctx, scenarioEvent{desc: rest})
	case "advance":
		d, err := time.ParseDuration(rest)
		if err != nil {
			return fmt.Errorf("bad duration: %w", err)
		}
		return s.Advance(ctx, d)
	case "expect":
		return expectStep(t, s, rest)
	default:
		return fmt.Errorf("unknown verb %q", verb)
	}
}

func expectStep(t *testing.T, s *Sync[scenarioCtx, scenarioEvent], rest string) error {
	kind, args, _ := strings.Cut(rest, " ")
	snap := s.Snapshot()
	switch kind {
	case "active":
		want := strings.Fields(args)
		got := make([]string, len(snap.ActiveStates))
		for i, st := range snap.ActiveStates {
			got[i] = string(st)
		}
		sort.Strings(want)
		sort.Strings(got)
		if strings.Join(want, ",") != strings.Join(got, ",") {
			t.Errorf("active = %v, want %v", got, want)
		}
		return nil
	case "status":
		if got := snap.Status.String(); got != args {
			t.Errorf("status = %s, want %s", got, args)
		}
		return nil
	default:
		return fmt.Errorf("unknown expect kind %q", kind)
	}
}
