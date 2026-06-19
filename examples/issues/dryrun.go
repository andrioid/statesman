package issues

import (
	"context"
	"fmt"
	"io"

	"github.com/andrioid/statesman"
)

// DryRunSyncIssue is a drop-in replacement for SyncIssue that never touches
// GitHub: it writes the comment it *would* post to w and reports Posted=false.
// It lets the coordinator run end-to-end (collect reads and the classify/
// investigate/fix/summarise agents still run for real) so prompt templates can
// be exercised against a live issue without mutating anything.
func DryRunSyncIssue(w io.Writer) func(context.Context, SyncInput) (SyncResult, error) {
	return func(_ context.Context, in SyncInput) (SyncResult, error) {
		marker := fmt.Sprintf("<!-- issues-bot:%d -->", in.Number)
		fmt.Fprintf(w, "[dry-run] would comment on %s/%s#%d (subject %s):\n%s\n",
			in.Owner, in.Repo, in.Number, in.IssueID, marker+"\n"+in.Comment)
		return SyncResult{Posted: false}, nil
	}
}

// RegisterDryRunSync overrides the machine's "sync" invoke with DryRunSyncIssue,
// reusing the generated input mapper and done/error event wiring. Call it after
// NewIssuesMachine and before Start. Every other invoke (including the live
// collect read) is left untouched.
func RegisterDryRunSync(m *statesman.Machine[Context, Event], w io.Writer) {
	m.RegisterInvoke("sync", statesman.PromiseActor[Context, Event, SyncInput, SyncResult](
		DryRunSyncIssue(w), Impl{}.SyncIssueInput,
		func(o SyncResult) Event { return SyncDone{Output: o} },
		func(e error) Event { return SyncError{Err: e} }))
}
