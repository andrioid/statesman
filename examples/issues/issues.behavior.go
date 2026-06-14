package issues

import (
	"github.com/andrioid/statesman"
	"github.com/andrioid/statesman/examples/issues/fix"
	"github.com/andrioid/statesman/examples/issues/investigate"
)

// maxRetries bounds the collect/sync retry loops; fixAttempts is the edit->test
// budget the fix sub-machine is seeded with.
const (
	maxRetries  = 3
	fixAttempts = 3
)

// Impl is the coordinator's behavior.
type Impl struct{}

// --- input mappers (promise + sub-machine seeding) ---

func (Impl) CollectIssueInput(ctx Context) CollectInput { return CollectInput{Number: ctx.Number} }

func (Impl) ClassifyIssueInput(ctx Context) ClassifyInput {
	return ClassifyInput{Number: ctx.Number, Title: ctx.Title, Body: ctx.Body}
}

func (Impl) SummariseIssueInput(ctx Context) SummariseInput {
	return SummariseInput{Number: ctx.Number, Category: ctx.Category, Findings: ctx.Findings, Patch: ctx.Patch}
}

func (Impl) SyncIssueInput(ctx Context) SyncInput {
	return SyncInput{Number: ctx.Number, Comment: ctx.Summary}
}

// InvestigateInput / FixInput seed each sub-machine's initial context from the
// coordinator — the machine analogue of a promise input mapper.
func (Impl) InvestigateInput(ctx Context) investigate.Context {
	return investigate.Context{ContextFields: investigate.ContextFields{Number: ctx.Number}}
}

func (Impl) FixInput(ctx Context) fix.Context {
	return fix.Context{ContextFields: fix.ContextFields{
		Number: ctx.Number, Findings: ctx.Findings, MaxAttempts: fixAttempts,
	}}
}

// --- actions ---

func (Impl) SavePackageOnCollectDone(ctx Context, evt CollectDone) ActionResult {
	f := ctx.ContextFields
	f.Title, f.Body = evt.Output.Title, evt.Output.Body
	return Assign{Fields: f}
}

func (Impl) SaveClassificationOnClassifyDone(ctx Context, evt ClassifyDone) ActionResult {
	f := ctx.ContextFields
	f.Category = evt.Output.Category
	return Assign{Fields: f}
}

func (Impl) SaveFindingsOnInvestigateDone(ctx Context, evt InvestigateDone) ActionResult {
	f := ctx.ContextFields
	f.Findings = evt.Output.Findings // evt.Output is the child's terminal Context
	return Assign{Fields: f}
}

func (Impl) SavePatchOnFixDone(ctx Context, evt FixDone) ActionResult {
	f := ctx.ContextFields
	f.Patch = evt.Output.Patch
	return Assign{Fields: f}
}

func (Impl) SaveSummaryOnSummariseDone(ctx Context, evt SummariseDone) ActionResult {
	f := ctx.ContextFields
	f.Summary = evt.Output.Comment
	return Assign{Fields: f}
}

func (Impl) RecordFailureOnCollectError(ctx Context, evt CollectError) ActionResult {
	return recordErr(ctx, evt.Err)
}

func (Impl) RecordFailureOnSyncError(ctx Context, evt SyncError) ActionResult {
	return recordErr(ctx, evt.Err)
}

func recordErr(ctx Context, err error) ActionResult {
	f := ctx.ContextFields
	if err != nil {
		f.LastError = err.Error()
	}
	return Assign{Fields: f}
}

func (Impl) NextAttemptOnCollectingBackingOff(ctx Context) ActionResult { return bump(ctx) }
func (Impl) NextAttemptOnSyncingBackingOff(ctx Context) ActionResult    { return bump(ctx) }

func bump(ctx Context) ActionResult {
	f := ctx.ContextFields
	f.Attempt++
	return Assign{Fields: f}
}

// --- guards ---

// HasAttemptsLeft is the timeout-edge budget guard (a timeout is always retryable).
// Reused on both collect and sync timeouts, so it is the generated context-only
// fallback.
func (Impl) HasAttemptsLeft(ctx Context) bool { return ctx.Attempt < maxRetries }

// ShouldRetry is the context-only fallback for the error edges (used when no
// per-callsite override applies).
func (Impl) ShouldRetry(ctx Context) bool { return ctx.Attempt < maxRetries }

// ShouldRetryOnCollectError / ...OnSyncError override the fallback to classify the
// error: only transient transport failures are worth a retry (a 4xx is not).
func (Impl) ShouldRetryOnCollectError(ctx Context, evt CollectError) bool {
	return ctx.Attempt < maxRetries && statesman.IsTransient(evt.Err)
}

func (Impl) ShouldRetryOnSyncError(ctx Context, evt SyncError) bool {
	return ctx.Attempt < maxRetries && statesman.IsTransient(evt.Err)
}

// IsActionableOnClassifyDone routes only bugs into investigate/fix; everything else
// goes straight to summarising.
func (Impl) IsActionableOnClassifyDone(ctx Context, evt ClassifyDone) bool {
	return evt.Output.Category == "bug"
}
