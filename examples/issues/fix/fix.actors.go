package fix

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	"github.com/andrioid/statesman/examples/issues/agent"
)

// EditInput / EditResult are the applyEdit promise adapter's I/O.
type EditInput struct {
	Number   int
	Findings string
}

type EditResult struct{ Patch string }

// TestInput / TestResult are the runTests promise adapter's I/O.
type TestInput struct{ Number int }

type TestResult struct {
	Passed bool
	Output string
}

const fixPrompt = `Propose a unified-diff patch that fixes GitHub issue #{{.Number}}, based on these findings:

{{.Findings}}

Output only the patch text.`

type fixVars struct {
	Number   int
	Findings string
}

// ApplyEdit runs the fixing agent and returns its proposed patch. ctx bounds the
// subprocess so a timeout edge can kill it.
func ApplyEdit(ctx context.Context, in EditInput) (EditResult, error) {
	out, err := agent.Invoke(ctx, "fix", fixPrompt, fixVars{in.Number, in.Findings})
	if err != nil {
		return EditResult{}, err
	}
	return EditResult{Patch: out}, nil
}

// RunTests execs the verification command; a zero exit is a pass. Set VERIFY_CMD
// to your test/verification command (e.g. "go test ./...").
func RunTests(ctx context.Context, in TestInput) (TestResult, error) {
	cmd := os.Getenv("VERIFY_CMD")
	if cmd == "" {
		return TestResult{}, errors.New("fix: set VERIFY_CMD to your verification command")
	}
	out, err := exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
	return TestResult{Passed: err == nil, Output: strings.TrimSpace(string(out))}, nil
}
