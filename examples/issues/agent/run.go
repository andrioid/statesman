// Package agent runs the issue-triage LLM steps through a user-configured
// command-line agent harness. Each verb (classify, summarise, analyse, fix)
// builds a prompt from a Go text/template and hands it to the harness named by
// the AGENT environment variable (or a per-verb AGENT_<VERB> override).
//
// The harness command is a template itself: the literal token {{prompt}} is
// replaced — as a single argv element, never via a shell — with the rendered
// prompt. If the command has no {{prompt}}, the prompt is fed on stdin instead.
//
//	AGENT='pi -p {{prompt}}'                 # prompt as an argument
//	AGENT='claude --print {{prompt}}'
//	AGENT_FIX='pi -p --thinking high {{prompt}}'   # per-verb override
//	AGENT='some-agent'                       # no placeholder -> prompt on stdin
//
// Default prompt templates live with each verb's actor; set AGENT_PROMPT_DIR to a
// directory of <verb>.tmpl files to override them without recompiling.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// Placeholder is the token in an AGENT command replaced with the rendered prompt.
const Placeholder = "{{prompt}}"

// Invoke renders verb's prompt (default template or an AGENT_PROMPT_DIR override)
// with data, resolves the agent command, and runs it, returning trimmed stdout.
func Invoke(ctx context.Context, verb, defaultTmpl string, data any) (string, error) {
	spec, err := resolveSpec(verb)
	if err != nil {
		return "", err
	}
	prompt, err := render(verb, defaultTmpl, data)
	if err != nil {
		return "", err
	}
	return Run(ctx, spec, prompt)
}

// Run executes a resolved agent command spec with the given prompt and returns
// trimmed stdout. ctx bounds the subprocess so a timeout edge can kill it.
func Run(ctx context.Context, spec, prompt string) (string, error) {
	argv, stdin, err := buildExec(spec, prompt)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("agent %q: %w", argv[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveSpec returns AGENT_<VERB> if set, else AGENT; only the base is required.
func resolveSpec(verb string) (string, error) {
	if s := strings.TrimSpace(os.Getenv("AGENT_" + strings.ToUpper(verb))); s != "" {
		return s, nil
	}
	if s := strings.TrimSpace(os.Getenv("AGENT")); s != "" {
		return s, nil
	}
	return "", fmt.Errorf("agent: set AGENT (or AGENT_%s) to your agent command, e.g. AGENT='pi -p {{prompt}}'",
		strings.ToUpper(verb))
}

// render parses and executes verb's prompt template against data. The template is
// the AGENT_PROMPT_DIR/<verb>.tmpl file when present, otherwise defaultTmpl.
func render(verb, defaultTmpl string, data any) (string, error) {
	text := defaultTmpl
	if dir := strings.TrimSpace(os.Getenv("AGENT_PROMPT_DIR")); dir != "" {
		path := filepath.Join(dir, verb+".tmpl")
		switch b, err := os.ReadFile(path); {
		case err == nil:
			text = string(b)
		case errors.Is(err, fs.ErrNotExist):
			// no override for this verb; fall through to the default
		default:
			return "", fmt.Errorf("agent: read prompt %s: %w", path, err)
		}
	}
	t, err := template.New(verb).Parse(text)
	if err != nil {
		return "", fmt.Errorf("agent: parse %s prompt: %w", verb, err)
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("agent: render %s prompt: %w", verb, err)
	}
	return buf.String(), nil
}

// buildExec splits the command spec into argv and substitutes Placeholder with
// prompt as a literal argv element (no shell). When the spec has no Placeholder,
// the prompt is returned as stdin instead.
func buildExec(spec, prompt string) (argv []string, stdin string, err error) {
	argv, err = splitArgs(spec)
	if err != nil {
		return nil, "", err
	}
	if len(argv) == 0 {
		return nil, "", errors.New("agent: empty command")
	}
	found := false
	for i, a := range argv {
		if strings.Contains(a, Placeholder) {
			argv[i] = strings.ReplaceAll(a, Placeholder, prompt)
			found = true
		}
	}
	if !found {
		stdin = prompt
	}
	return argv, stdin, nil
}

// splitArgs is a minimal POSIX-ish word splitter: whitespace separates words,
// single and double quotes group (no escape or variable expansion inside, since
// the prompt is substituted post-split and never shell-evaluated).
func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
			i++
		case c == '\'' || c == '"':
			inWord = true
			quote := c
			i++
			for i < len(s) && s[i] != quote {
				cur.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("agent: unterminated %c quote in command", quote)
			}
			i++ // skip the closing quote
		default:
			inWord = true
			cur.WriteByte(c)
			i++
		}
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}
