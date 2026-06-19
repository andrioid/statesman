package issues

import (
	"context"
	"fmt"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/examples/issues/agent"
	"github.com/andrioid/statesman/examples/issues/fix"
	"github.com/andrioid/statesman/examples/issues/github"
	"github.com/andrioid/statesman/examples/issues/investigate"
)

// --- collect: read the issue from GitHub (GraphQL) ---

type CollectInput struct{ Number int }

type CollectResult struct {
	Title   string
	Body    string
	IssueID string // node id, threaded to sync as the comment subject
	Owner   string // resolved repo, saved into context for sync
	Repo    string
}

// CollectIssue reads the issue via GitHub GraphQL in one round-trip, resolving
// the target repo from the environment or git remote. The ctx-bound token/repo
// subprocesses honor the CollectAttemptTimeout edge: a state exit cancels ctx and
// kills any spawned gh/git process.
func CollectIssue(ctx context.Context, in CollectInput) (CollectResult, error) {
	owner, repo, err := github.Repo(ctx)
	if err != nil {
		return CollectResult{}, err
	}
	client, err := github.NewClient(ctx)
	if err != nil {
		return CollectResult{}, err
	}
	iss, err := github.GetIssue(ctx, client, owner, repo, in.Number)
	if err != nil {
		return CollectResult{}, err
	}
	return CollectResult{Title: iss.Title, Body: iss.Body, IssueID: iss.ID, Owner: owner, Repo: repo}, nil
}

// --- classify: one-shot LLM ---

type ClassifyInput struct {
	Number int
	Title  string
	Body   string
}

type ClassifyResult struct{ Category string } // e.g. "bug", "question", "duplicate"

const classifyPrompt = `You are triaging GitHub issue #{{.Number}}.

Title: {{.Title}}

Body:
{{.Body}}

Classify it as exactly ONE lowercase word from: bug, question, duplicate, invalid. Reply with only that single word.`

type classifyVars struct {
	Number int
	Title  string
	Body   string
}

func ClassifyIssue(ctx context.Context, in ClassifyInput) (ClassifyResult, error) {
	out, err := agent.Invoke(ctx, "classify", classifyPrompt, classifyVars{in.Number, in.Title, in.Body})
	if err != nil {
		return ClassifyResult{}, err
	}
	return ClassifyResult{Category: out}, nil
}

// --- summarise: one-shot LLM ---

type SummariseInput struct {
	Number   int
	Category string
	Findings string
	Patch    string
}

type SummariseResult struct{ Comment string }

const summarisePrompt = `Write a concise maintainer comment (2-4 sentences) for GitHub issue #{{.Number}} summarising the triage outcome.

Category: {{.Category}}
{{if .Findings}}
Findings:
{{.Findings}}
{{end}}{{if .Patch}}
Proposed patch:
{{.Patch}}
{{end}}
Reply with only the comment text.`

type summariseVars struct {
	Number   int
	Category string
	Findings string
	Patch    string
}

func SummariseIssue(ctx context.Context, in SummariseInput) (SummariseResult, error) {
	out, err := agent.Invoke(ctx, "summarise", summarisePrompt, summariseVars{in.Number, in.Category, in.Findings, in.Patch})
	if err != nil {
		return SummariseResult{}, err
	}
	return SummariseResult{Comment: out}, nil
}

// --- sync: write back to GitHub (GraphQL), idempotently ---

type SyncInput struct {
	Number  int
	Owner   string
	Repo    string
	IssueID string
	Comment string
}

type SyncResult struct{ Posted bool }

// SyncIssue posts the summary as a comment via GraphQL. Retries are at-least-once,
// so a timed-out post may have landed: the body carries a per-issue marker and a
// prior comment with that marker short-circuits the write. Idempotency lives here,
// in the adapter — not in the chart.
func SyncIssue(ctx context.Context, in SyncInput) (SyncResult, error) {
	client, err := github.NewClient(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	marker := fmt.Sprintf("<!-- issues-bot:%d -->", in.Number)
	exists, err := github.CommentExists(ctx, client, in.Owner, in.Repo, in.Number, marker)
	if err != nil {
		return SyncResult{}, err
	}
	if exists {
		return SyncResult{Posted: false}, nil // already synced on a prior attempt
	}
	if err := github.PostComment(ctx, client, in.IssueID, marker+"\n"+in.Comment); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{Posted: true}, nil
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
