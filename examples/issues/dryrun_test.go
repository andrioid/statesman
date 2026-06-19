package issues

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/statesmantest"
)

// The dry-run sync actor reports Posted=false and writes the comment it would
// have posted — and, having no GraphQL client, cannot reach GitHub.
func TestDryRunSyncIssueNeverPosts(t *testing.T) {
	var buf bytes.Buffer
	res, err := DryRunSyncIssue(&buf)(context.Background(),
		SyncInput{Number: 42, Owner: "octocat", Repo: "Hello-World", IssueID: "I_x", Comment: "fixed in #99"})
	if err != nil {
		t.Fatalf("DryRunSyncIssue: %v", err)
	}
	if res.Posted {
		t.Fatal("Posted = true, want false (dry-run)")
	}
	out := buf.String()
	if !strings.Contains(out, "octocat/Hello-World#42") || !strings.Contains(out, "fixed in #99") ||
		!strings.Contains(out, "<!-- issues-bot:42 -->") {
		t.Fatalf("dry-run output missing repo/comment/marker: %q", out)
	}
}

// With sync overridden by the dry-run actor, the full coordinator still runs to
// Finished/Done — the summary is "posted" only to the buffer, never to GitHub.
// Uses a real-clock machine (the dry-run sync resolves on a goroutine, which the
// virtual-clock Sync harness does not pump) and waits for a terminal snapshot.
func TestDryRunSyncCompletesPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var buf bytes.Buffer
	m := NewIssuesMachine(Impl{})
	m.SetInitialContext(Context{ContextFields: ContextFields{Number: 7}})
	// non-bug path: investigate/fix are skipped, so only collect/classify/
	// summarise need faking; collect carries the repo identity sync consumes.
	m.RegisterInvoke("collect", statesmantest.FakeActor[Context, Event](
		CollectDone{Output: CollectResult{Title: "typo in docs", Body: "...", IssueID: "I_7", Owner: "octocat", Repo: "Hello-World"}}))
	m.RegisterInvoke("classify", statesmantest.FakeActor[Context, Event](
		ClassifyDone{Output: ClassifyResult{Category: "question"}}))
	m.RegisterInvoke("summarise", statesmantest.FakeActor[Context, Event](
		SummariseDone{Output: SummariseResult{Comment: "answered: see the FAQ"}}))
	RegisterDryRunSync(m, &buf)

	term := make(chan statesman.Snapshot[Context], 1)
	m.Subscribe(ctx, func(s statesman.Snapshot[Context]) {
		if s.Status == statesman.StatusDone || s.Status == statesman.StatusError {
			select {
			case term <- s:
			default:
			}
		}
	})
	if err := m.Start(ctx, "issue-7"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.Close()

	var snap statesman.Snapshot[Context]
	select {
	case snap = <-term:
	case <-ctx.Done():
		cur := m.Snapshot()
		t.Fatalf("timeout before terminal; last = %v/%v", cur.ActiveStates, cur.Status)
	}
	if snap.Status != statesman.StatusDone || !active(snap, States.Finished) {
		t.Fatalf("final = %v/%v, want finished/Done", snap.ActiveStates, snap.Status)
	}
	out := buf.String()
	if !strings.Contains(out, "octocat/Hello-World#7") || !strings.Contains(out, "answered: see the FAQ") {
		t.Fatalf("dry-run did not capture the summary comment: %q", out)
	}
}
