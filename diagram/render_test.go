package diagram_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/diagram"
	"github.com/andrioid/statesman/schema"
)

var update = flag.Bool("update", false, "update golden files")

var fixtures = []string{"simple", "order", "player"}

func load(t *testing.T, name string) *statesman.Definition {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".machine.json"))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	def, err := schema.Load(data)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return def
}

func golden(t *testing.T, file, got string) {
	t.Helper()
	path := filepath.Join("testdata", file)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", file, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update)", file, err)
	}
	if string(want) != got {
		t.Errorf("%s mismatch:\n--- got ---\n%s\n--- want ---\n%s", file, got, want)
	}
}

func TestMermaidGolden(t *testing.T) {
	for _, f := range fixtures {
		golden(t, f+".mmd", diagram.Mermaid(load(t, f)))
	}
}

func TestTextGolden(t *testing.T) {
	for _, f := range fixtures {
		golden(t, f+".txt", diagram.Text(load(t, f)))
	}
}
