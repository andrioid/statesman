package fix

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// ApplyEdit execs the fixing agent (shell), passing the findings on stdin, and
// returns its patch. Set ISSUES_AGENT to your agent CLI; called as
// `$ISSUES_AGENT fix <number>`.
func ApplyEdit(ctx context.Context, in EditInput) (EditResult, error) {
	agent := os.Getenv("ISSUES_AGENT")
	if agent == "" {
		return EditResult{}, errors.New("fix: set ISSUES_AGENT to your fixing agent command")
	}
	cmd := exec.CommandContext(ctx, agent, "fix", strconv.Itoa(in.Number))
	cmd.Stdin = strings.NewReader(in.Findings)
	out, err := cmd.Output()
	if err != nil {
		return EditResult{}, fmt.Errorf("fix agent: %w", err)
	}
	return EditResult{Patch: strings.TrimSpace(string(out))}, nil
}

// RunTests execs the test command; a zero exit is a pass. Set ISSUES_TEST to your
// test command (e.g. "go test ./...").
func RunTests(ctx context.Context, in TestInput) (TestResult, error) {
	test := os.Getenv("ISSUES_TEST")
	if test == "" {
		return TestResult{}, errors.New("fix: set ISSUES_TEST to your test command")
	}
	out, err := exec.CommandContext(ctx, "sh", "-c", test).CombinedOutput()
	return TestResult{Passed: err == nil, Output: strings.TrimSpace(string(out))}, nil
}
