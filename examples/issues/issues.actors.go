package issues

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/examples/issues/fix"
	"github.com/andrioid/statesman/examples/issues/investigate"
)

// --- collect: read the issue from GitHub (gh) ---

type CollectInput struct{ Number int }

type CollectResult struct {
	Title string
	Body  string
}

// CollectIssue reads the issue via gh. CommandContext honors the CollectAttemptTimeout
// edge (state exit cancels ctx → process killed).
func CollectIssue(ctx context.Context, in CollectInput) (CollectResult, error) {
	out, err := exec.CommandContext(ctx, "gh", "issue", "view", strconv.Itoa(in.Number),
		"--json", "title,body", "-t", "{{.title}}\x1f{{.body}}").Output()
	if err != nil {
		return CollectResult{}, fmt.Errorf("gh issue view: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "\x1f", 2)
	r := CollectResult{Title: parts[0]}
	if len(parts) == 2 {
		r.Body = parts[1]
	}
	return r, nil
}

// --- classify: one-shot LLM ---

type ClassifyInput struct {
	Number int
	Title  string
	Body   string
}

type ClassifyResult struct{ Category string } // e.g. "bug", "question", "duplicate"

func ClassifyIssue(ctx context.Context, in ClassifyInput) (ClassifyResult, error) {
	out, err := agent(ctx, in.Title+"\n\n"+in.Body, "classify", strconv.Itoa(in.Number))
	if err != nil {
		return ClassifyResult{}, err
	}
	return ClassifyResult{Category: strings.TrimSpace(out)}, nil
}

// --- summarise: one-shot LLM ---

type SummariseInput struct {
	Number   int
	Category string
	Findings string
	Patch    string
}

type SummariseResult struct{ Comment string }

func SummariseIssue(ctx context.Context, in SummariseInput) (SummariseResult, error) {
	out, err := agent(ctx, in.Findings+"\n\n"+in.Patch, "summarise", strconv.Itoa(in.Number))
	if err != nil {
		return SummariseResult{}, err
	}
	return SummariseResult{Comment: strings.TrimSpace(out)}, nil
}

// --- sync: write back to GitHub (gh), idempotently ---

type SyncInput struct {
	Number  int
	Comment string
}

type SyncResult struct{ Posted bool }

// SyncIssue posts the summary as a comment. Retries are at-least-once, so a
// timed-out post may have landed: the body carries a per-issue marker and a prior
// comment with that marker short-circuits the write. Idempotency lives here, in the
// adapter — not in the chart.
func SyncIssue(ctx context.Context, in SyncInput) (SyncResult, error) {
	marker := fmt.Sprintf("<!-- issues-bot:%d -->", in.Number)
	existing, err := exec.CommandContext(ctx, "gh", "issue", "view", strconv.Itoa(in.Number), "--comments").Output()
	if err != nil {
		return SyncResult{}, fmt.Errorf("gh issue view --comments: %w", err)
	}
	if strings.Contains(string(existing), marker) {
		return SyncResult{Posted: false}, nil // already synced on a prior attempt
	}
	if err := exec.CommandContext(ctx, "gh", "issue", "comment", strconv.Itoa(in.Number),
		"--body", marker+"\n"+in.Comment).Run(); err != nil {
		return SyncResult{}, fmt.Errorf("gh issue comment: %w", err)
	}
	return SyncResult{Posted: true}, nil
}

// agent execs the configured LLM agent CLI (the "agent from shell"), feeding the
// payload on stdin and returning stdout.
func agent(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := os.Getenv("ISSUES_AGENT")
	if cmd == "" {
		return "", errors.New("issues: set ISSUES_AGENT to your LLM agent command")
	}
	c := exec.CommandContext(ctx, cmd, args...)
	c.Stdin = strings.NewReader(stdin)
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", cmd, strings.Join(args, " "), err)
	}
	return string(out), nil
}

// --- sub-machine factories (fromMachine src) ---

// Investigate builds a fresh investigate sub-machine per spawn; the coordinator
// seeds its context via InvestigateInput.
func Investigate() *statesman.Machine[investigate.Context, investigate.Event] {
	return investigate.NewInvestigateMachine(investigate.Impl{})
}

// Fix builds a fresh fix sub-machine per spawn; seeded via FixInput.
func Fix() *statesman.Machine[fix.Context, fix.Event] {
	return fix.NewFixMachine(fix.Impl{})
}
