package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		err  bool
	}{
		{`pi -p {{prompt}}`, []string{"pi", "-p", "{{prompt}}"}, false},
		{`pi --system "you are a triager" -p {{prompt}}`, []string{"pi", "--system", "you are a triager", "-p", "{{prompt}}"}, false},
		{`llm -m 'gpt-4o' {{prompt}}`, []string{"llm", "-m", "gpt-4o", "{{prompt}}"}, false},
		{`  spaced   out  `, []string{"spaced", "out"}, false},
		{`oops "unterminated`, nil, true},
	}
	for _, c := range cases {
		got, err := splitArgs(c.in)
		if (err != nil) != c.err {
			t.Errorf("splitArgs(%q) err = %v, want err=%v", c.in, err, c.err)
			continue
		}
		if c.err {
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("splitArgs(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitArgs(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestBuildExecSubstitutesPlaceholder(t *testing.T) {
	argv, stdin, err := buildExec("pi -p {{prompt}}", "do the thing")
	if err != nil {
		t.Fatal(err)
	}
	if stdin != "" {
		t.Fatalf("stdin = %q, want empty (placeholder present)", stdin)
	}
	want := []string{"pi", "-p", "do the thing"}
	if len(argv) != len(want) || argv[2] != "do the thing" {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
}

func TestBuildExecEmbeddedPlaceholder(t *testing.T) {
	argv, _, err := buildExec("agent --prompt={{prompt}}", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if argv[1] != "--prompt=hi" {
		t.Fatalf("argv[1] = %q, want --prompt=hi", argv[1])
	}
}

func TestBuildExecNoPlaceholderUsesStdin(t *testing.T) {
	argv, stdin, err := buildExec("some-agent", "the prompt")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 1 || argv[0] != "some-agent" {
		t.Fatalf("argv = %q, want [some-agent]", argv)
	}
	if stdin != "the prompt" {
		t.Fatalf("stdin = %q, want the prompt", stdin)
	}
}

func TestResolveSpecPrecedence(t *testing.T) {
	t.Setenv("AGENT", "base {{prompt}}")
	t.Setenv("AGENT_CLASSIFY", "")
	if got, _ := resolveSpec("classify"); got != "base {{prompt}}" {
		t.Fatalf("resolveSpec classify = %q, want base (fallback)", got)
	}
	t.Setenv("AGENT_CLASSIFY", "override {{prompt}}")
	if got, _ := resolveSpec("classify"); got != "override {{prompt}}" {
		t.Fatalf("resolveSpec classify = %q, want override", got)
	}
	// fix has no override -> still falls back to AGENT
	if got, _ := resolveSpec("fix"); got != "base {{prompt}}" {
		t.Fatalf("resolveSpec fix = %q, want base", got)
	}
}

func TestResolveSpecRequiresAgent(t *testing.T) {
	t.Setenv("AGENT", "")
	t.Setenv("AGENT_FIX", "")
	if _, err := resolveSpec("fix"); err == nil {
		t.Fatal("resolveSpec: want error when AGENT and AGENT_FIX both unset")
	}
}

func TestRenderDefault(t *testing.T) {
	t.Setenv("AGENT_PROMPT_DIR", "")
	out, err := render("classify", "issue #{{.Number}}: {{.Title}}", struct {
		Number int
		Title  string
	}{7, "crash"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "issue #7: crash" {
		t.Fatalf("render = %q, want \"issue #7: crash\"", out)
	}
}

func TestRenderOverrideFromDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "classify.tmpl"), []byte("OVERRIDE {{.Title}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_PROMPT_DIR", dir)
	// classify is overridden; summarise has no file -> falls back to its default
	got, err := render("classify", "DEFAULT {{.Title}}", struct{ Title string }{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "OVERRIDE x" {
		t.Fatalf("render classify = %q, want OVERRIDE x", got)
	}
	got, err = render("summarise", "DEFAULT {{.Title}}", struct{ Title string }{"y"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "DEFAULT y" {
		t.Fatalf("render summarise = %q, want DEFAULT y (no override file)", got)
	}
}

func TestRunPlaceholderArg(t *testing.T) {
	// echo receives the prompt as a single argv element and prints it back.
	out, err := Run(context.Background(), "echo {{prompt}}", "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello world" {
		t.Fatalf("Run = %q, want \"hello world\"", out)
	}
}

func TestRunStdin(t *testing.T) {
	// no placeholder -> prompt on stdin -> cat echoes it.
	out, err := Run(context.Background(), "cat", "piped prompt")
	if err != nil {
		t.Fatal(err)
	}
	if out != "piped prompt" {
		t.Fatalf("Run = %q, want \"piped prompt\"", out)
	}
}

func TestInvokeEndToEnd(t *testing.T) {
	t.Setenv("AGENT", "cat") // no placeholder -> rendered prompt on stdin -> echoed back
	t.Setenv("AGENT_PROMPT_DIR", "")
	out, err := Invoke(context.Background(), "classify", "issue #{{.Number}} is a {{.Kind}}", struct {
		Number int
		Kind   string
	}{42, "bug"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "issue #42 is a bug" {
		t.Fatalf("Invoke = %q, want \"issue #42 is a bug\"", out)
	}
}
