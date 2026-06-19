// Command issuesctl runs the issue-triage coordinator against a real GitHub
// issue. With --dry-run (the default) the only GitHub write — posting the
// summary comment — is suppressed and printed instead, so you can drive the
// whole machine and iterate on the LLM prompt templates without mutating
// anything.
//
// Reads (the issue body) still hit GitHub live, and classify/investigate/fix/
// summarise still exec your $AGENT harness (with the {{prompt}} placeholder), so set it.
// The token resolves from $GITHUB_TOKEN or `gh auth token`; the repo from
// --repo, $GITHUB_REPOSITORY, or the git origin remote.
//
//	go run ./cmd/issuesctl 42                 # dry run (no writes)
//	go run ./cmd/issuesctl --dry-run=false 42 # actually post the comment
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/examples/issues"
)

func main() {
	dryRun := flag.Bool("dry-run", true, "suppress the GitHub comment write; print it instead")
	repo := flag.String("repo", "", `target "owner/repo" (overrides $GITHUB_REPOSITORY and the git remote)`)
	timeout := flag.Duration("timeout", 5*time.Minute, "overall deadline for the run")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: issuesctl [flags] <issue-number>\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	n, err := strconv.Atoi(flag.Arg(0))
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "issue number must be a positive integer, got %q\n", flag.Arg(0))
		os.Exit(2)
	}
	if *repo != "" {
		os.Setenv("GITHUB_REPOSITORY", *repo)
	}
	if os.Getenv("AGENT") == "" {
		fmt.Fprintln(os.Stderr, "warning: $AGENT is unset — classify/summarise/analyse/fix will fail")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	m := issues.NewIssuesMachine(issues.Impl{})
	if *dryRun {
		issues.RegisterDryRunSync(m, os.Stdout)
		fmt.Println("[dry-run] GitHub writes suppressed; collect reads and agents run live")
	}
	m.SetInitialContext(issues.Context{ContextFields: issues.ContextFields{Number: n}})

	term := make(chan statesman.Snapshot[issues.Context], 1)
	var lastStates string
	m.Subscribe(ctx, func(s statesman.Snapshot[issues.Context]) {
		if cur := fmt.Sprint(s.ActiveStates); cur != lastStates {
			fmt.Printf("→ %v\n", s.ActiveStates)
			lastStates = cur
		}
		if s.Status == statesman.StatusDone || s.Status == statesman.StatusError {
			select {
			case term <- s:
			default:
			}
		}
	})

	if err := m.Start(ctx, fmt.Sprintf("issue-%d", n)); err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	defer m.Close()

	select {
	case s := <-term:
		report(s)
		if s.Status == statesman.StatusError || endedFailed(s) {
			os.Exit(1)
		}
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "run did not finish:", ctx.Err())
		report(m.Snapshot())
		os.Exit(1)
	}
}

func report(s statesman.Snapshot[issues.Context]) {
	c := s.Context
	fmt.Println("\n── result ───────────────────────")
	fmt.Printf("status:    %s\n", s.Status)
	if c.Owner != "" {
		fmt.Printf("issue:     %s/%s#%d\n", c.Owner, c.Repo, c.Number)
	} else {
		fmt.Printf("issue:     #%d\n", c.Number)
	}
	fmt.Printf("title:     %s\n", c.Title)
	fmt.Printf("category:  %s\n", c.Category)
	if c.Findings != "" {
		fmt.Printf("findings:  %s\n", c.Findings)
	}
	if c.Patch != "" {
		fmt.Printf("patch:     %s\n", c.Patch)
	}
	if c.Summary != "" {
		fmt.Printf("summary:   %s\n", c.Summary)
	}
	if c.LastError != "" {
		fmt.Printf("last error: %s\n", c.LastError)
	}
	if s.ErrorReason != nil {
		fmt.Printf("error:     %v\n", s.ErrorReason)
	}
}

// endedFailed reports whether the machine settled in its `failed` state. That is
// a final state, so Status is Done, but it's a triage failure the CLI signals
// with a non-zero exit.
func endedFailed(s statesman.Snapshot[issues.Context]) bool {
	for _, id := range s.ActiveStates {
		if id == issues.States.Failed {
			return true
		}
	}
	return false
}
